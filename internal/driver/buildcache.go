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
// what a module compiles to, the module's name and source, and the keys of the modules it depends on, which is made in
// the order of the graph so that each is known by the time it is needed. So it changes with the source of the module and
// of everything it reaches, and with the set of modules it reaches: a bare name that now resolves to a declaration in
// another module is a dependency the module did not have, which makes a different key.
func moduleKeys(store *cache.Cache, o Options, proj *ast.Project, graph *project.Graph, digests []cache.Key) []cache.Key {
	keys := make([]cache.Key, len(proj.Modules))
	for _, id := range graph.Order() {
		h := cache.NewHasher()
		h.String(keyDomain)
		h.Key(store.Compiler())
		h.Bool(o.CountAllocations)
		h.String(proj.Modules[id].LinkPath())
		h.Key(digests[id])
		deps := graph.Deps[id]
		h.Int(len(deps))
		for _, d := range deps {
			h.Key(keys[d.Module])
		}
		keys[id] = h.Sum()
	}
	return keys
}

// consultCache finds the keys, reads every module's entry and decides what the compile can skip. A module is a hit if its
// entry is whole and holds what this compile needs. A hit's generate does not run. Its check runs only if some module
// that is not a hit reaches it, because that module's generate reads the bodies and check results of the generics it
// reaches, and a module a hit does not reach needs nothing of it. Nothing that can report an error is skipped for a module
// that has one: only a module that compiled cleanly has an entry, and its key says that nothing it reads has changed.
func (p *modulePhase) consultCache() {
	n := len(p.proj.Modules)
	jobs := p.options.Jobs
	digests := make([]cache.Key, n)
	par.ForEachIndex(n, jobs, func(i int) { digests[i] = sourceDigest(p.proj.Modules[i]) })
	p.keys = moduleKeys(p.options.Cache, p.options, p.proj, p.graph, digests)
	p.cached = make([]*cachedModule, n)
	par.ForEachIndex(n, jobs, func(i int) {
		c := p.probe(ast.ModuleID(i))
		p.cached[i] = c
		if c != nil {
			p.units[i+1] = c.unit
			p.outputs[i] = moduleOutput{usesArrays: c.usesArrays, irText: c.irText}
		}
	})
	p.needCheck = p.reachedByMisses()
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

// reachedByMisses is, for each module, whether it is a miss or a module that a miss reaches: the modules whose check has to
// run.
func (p *modulePhase) reachedByMisses() []bool {
	need := make([]bool, len(p.cached))
	var pending []ast.ModuleID
	for i, c := range p.cached {
		if c == nil {
			need[i] = true
			pending = append(pending, ast.ModuleID(i))
		}
	}
	for len(pending) > 0 {
		m := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, d := range p.graph.Deps[m] {
			if !need[d.Module] {
				need[d.Module] = true
				pending = append(pending, d.Module)
			}
		}
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
	}
	return s
}
