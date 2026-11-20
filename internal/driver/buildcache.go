package driver

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/backend"
	"minimaxon/internal/cache"
	"minimaxon/internal/par"
	"minimaxon/internal/project"
)

// The build cache keeps what a module's generate made, so that a compile after an edit lowers and emits only the modules
// the edit can have changed (README, "Build cache"). This file is what the driver adds to the store of internal/cache: what
// an entry holds, and the key that says when it is still right.

// CacheStats is what a compile did with the build cache.
type CacheStats struct {
	// Hits are the modules whose code came from the cache, and Misses the others, which were lowered and emitted.
	Hits, Misses int
	// ChecksSkipped are the modules whose check did not run, because neither they nor any module that reaches them
	// needed it.
	ChecksSkipped int
	// WriteFailures are the entries that could not be stored, which never fail the compile.
	WriteFailures int
	// BytesRead and BytesWritten are the sizes of the entries that were read and written.
	BytesRead, BytesWritten int64
	// Modules is what the compile did with each module, in module order.
	Modules []ModuleCache
}

// ModuleCache is what a compile did with the cache for one module, by the module's link path.
type ModuleCache struct {
	Module string
	// Hit is whether the module's code came from the cache, and Checked whether its check ran.
	Hit, Checked bool
	// Summary is the module's export summary (see summary.go): a digest of what the modules that reach it can depend on, with
	// those of the modules it depends on folded in. ReadThrough is whether those modules read its check results.
	Summary     cache.Key
	ReadThrough bool
}

// cachedModule is what the cache keeps of a module's generate: its code unit and what the tail of the compile reads of its
// output. A module that is refused, or that has an error, has no entry, so none of its diagnostics is kept.
type cachedModule struct {
	unit       *backend.CodeUnit
	usesArrays bool
	// hasIr is whether irText is the module's IR; it is only kept by a compile that asked for it.
	hasIr  bool
	irText string
}

func (c cachedModule) encode() []byte {
	var e cache.Encoder
	e.Bool(c.usesArrays)
	e.Bool(c.hasIr)
	e.Text(c.irText)
	c.unit.Encode(&e)
	return e.Bytes
}

// decodeCachedModule reads an entry, or says it is not one: a payload that does not hold a unit is a miss.
func decodeCachedModule(payload []byte, proj *ast.Project) (*cachedModule, bool) {
	d := cache.NewDecoder(payload)
	c := &cachedModule{usesArrays: d.Bool(), hasIr: d.Bool(), irText: d.Text()}
	c.unit = backend.DecodeUnit(d, proj)
	if d.Done() != nil {
		return nil, false
	}
	return c, true
}

// keyDomain says what a key names, so that a hash of other things never meets one.
const keyDomain = "minimaxon module unit"

// sourceDigest is the hash of a module's text and of where its files end, which is all of the module's source that its
// code depends on (a file's name is only where a diagnostic says it is).
func sourceDigest(m *ast.Module) cache.Key {
	h := cache.NewHasher()
	files := m.Positions.Files()
	h.Int(len(files))
	for _, f := range files {
		h.Int(f.End - f.Begin)
	}
	h.String(m.Tree.Text)
	return h.Sum()
}

// moduleKeys are the keys of the modules' entries, by module. A module's key holds the compiler, the flags that change
// what a module compiles to, the module's name and source, and the export summaries of the modules it depends on, which
// say what the module can read of them and of what they reach. So it changes with the source of the module and with
// anything the modules it reaches show, but not with an edit that none of them shows, such as the body of a function that
// is not generic. It changes too with the set of modules it depends on: a bare name that now resolves to a declaration in
// another module is a dependency the module did not have.
func moduleKeys(store *cache.Cache, o Options, proj *ast.Project, graph *project.Graph, digests []cache.Key, summaries []moduleSummary) []cache.Key {
	keys := make([]cache.Key, len(proj.Modules))
	for id, m := range proj.Modules {
		h := cache.NewHasher()
		h.String(keyDomain)
		h.Key(store.Compiler())
		h.Bool(o.CountAllocations)
		h.String(m.LinkPath())
		h.Key(digests[id])
		deps := graph.Deps[id]
		h.Int(len(deps))
		for _, d := range deps {
			h.Key(summaries[d.Module].digest)
		}
		keys[id] = h.Sum()
	}
	return keys
}

// consultCache finds the keys, reads every module's entry and decides what the compile can skip. A module is a hit if its
// entry is whole and holds what this compile needs. A hit's generate does not run. Its check runs only if some module
// that is not a hit reaches it and reads its check results, which is what a module that declares a generic, an exported
// constant or a field's default lets another do. Nothing that can report an error is skipped for a module that has one:
// only a module that compiled cleanly has an entry, and its key says that nothing it reads has changed.
func (p *modulePhase) consultCache() {
	n := len(p.proj.Modules)
	jobs := p.options.Jobs
	digests := make([]cache.Key, n)
	par.ForEachIndex(n, jobs, func(i int) { digests[i] = sourceDigest(p.proj.Modules[i]) })
	p.summaries = summarize(p.proj, p.graph, jobs)
	p.keys = moduleKeys(p.options.Cache, p.options, p.proj, p.graph, digests, p.summaries)
	p.cached = make([]*cachedModule, n)
	par.ForEachIndex(n, jobs, func(i int) {
		c := p.probe(ast.ModuleID(i))
		p.cached[i] = c
		if c != nil {
			p.units[i+1] = c.unit
			p.outputs[i] = moduleOutput{usesArrays: c.usesArrays, irText: c.irText}
		}
	})
	p.needCheck = p.checksNeeded()
}

// probe is the module's entry, or nil if there is none that this compile can use.
func (p *modulePhase) probe(id ast.ModuleID) *cachedModule {
	payload, found := p.options.Cache.Get(p.keys[id])
	if !found {
		return nil
	}
	c, ok := decodeCachedModule(payload, p.proj)
	if !ok || (p.options.EmitIr && !c.hasIr) {
		return nil
	}
	return c
}

// checksNeeded is, for each module, whether its check has to run: a miss is checked, and so is a hit that a miss reaches and
// that shows its check results to the modules that reach it (its summary says so). A miss reads the check results of a
// module in its whole closure, not only of the modules it names, because it can instantiate a generic that a signature of
// a module it names leads to, and a hit that shows none is not checked however many misses reach it.
func (p *modulePhase) checksNeeded() []bool {
	reached := make([]bool, len(p.cached))
	var pending []ast.ModuleID
	for i, c := range p.cached {
		if c == nil {
			reached[i] = true
			pending = append(pending, ast.ModuleID(i))
		}
	}
	for len(pending) > 0 {
		m := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, d := range p.graph.Deps[m] {
			if !reached[d.Module] {
				reached[d.Module] = true
				pending = append(pending, d.Module)
			}
		}
	}
	need := make([]bool, len(reached))
	for i := range need {
		need[i] = p.cached[i] == nil || (reached[i] && p.summaries[i].readThrough)
	}
	return need
}

// hit is whether the module's code came from the cache.
func (p *modulePhase) hit(id ast.ModuleID) bool { return p.cached != nil && p.cached[id] != nil }

// skipsCheck is whether the module's check does not run.
func (p *modulePhase) skipsCheck(id ast.ModuleID) bool { return p.needCheck != nil && !p.needCheck[id] }

// remember stores the module's code and what the tail reads of its output. It is called only for a module with no
// refusal and no error, so every entry is one that a later compile may use.
func (p *modulePhase) remember(id ast.ModuleID, unit *backend.CodeUnit, out *moduleOutput) {
	if p.keys == nil {
		return
	}
	entry := cachedModule{unit: unit, usesArrays: out.usesArrays, hasIr: p.options.EmitIr, irText: out.irText}
	if err := p.options.Cache.Put(p.keys[id], entry.encode()); err != nil {
		p.writeFailures.Add(1)
	}
}

// cacheStats is what the compile did with the cache, as far as it has gone.
func (p *modulePhase) cacheStats() CacheStats {
	s := CacheStats{
		WriteFailures: int(p.writeFailures.Load()),
		BytesRead:     p.options.Cache.BytesRead() - p.cacheRead,
		BytesWritten:  p.options.Cache.BytesWritten() - p.cacheWritten,
	}
	for i, c := range p.cached {
		if c != nil {
			s.Hits++
		} else {
			s.Misses++
		}
		if !p.needCheck[i] {
			s.ChecksSkipped++
		}
		s.Modules = append(s.Modules, ModuleCache{
			Module: p.proj.Modules[i].LinkPath(), Hit: c != nil, Checked: p.needCheck[i],
			Summary: p.summaries[i].digest, ReadThrough: p.summaries[i].readThrough,
		})
	}
	return s
}
