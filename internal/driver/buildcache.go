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
	usesBytes  bool
	usesOs     bool
	// hasIr is whether irText is the module's IR; it is only kept by a compile that asked for it.
	hasIr  bool
	irText string
}

func (c cachedModule) encode() []byte {
	var e cache.Encoder
	e.Bool(c.usesArrays)
	e.Bool(c.usesBytes)
	e.Bool(c.usesOs)
	e.Bool(c.hasIr)
	e.Text(c.irText)
	c.unit.Encode(&e)
	return e.Bytes
}

// decodeCachedModule reads an entry, or says it is not one: a payload that does not hold a unit is a miss.
func decodeCachedModule(payload []byte, proj *ast.Project) (*cachedModule, bool) {
	d := cache.NewDecoder(payload)
	c := &cachedModule{usesArrays: d.Bool(), usesBytes: d.Bool(), usesOs: d.Bool(), hasIr: d.Bool(), irText: d.Text()}
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

// moduleKey is the key of a module's entry. It holds the compiler, the flags that change what a module compiles to, the
// module's name and source, and the export summaries of the modules it depends on (deps, in module order), which say what
// the module can read of them and of what they reach. So it changes with the source of the module and with anything the
// modules it reaches show, but not with an edit that none of them shows, such as the body of a function that is not
// generic. It changes too with the set of modules it depends on: a name that now resolves to a declaration in another
// module is a dependency the module did not have.
func moduleKey(store *cache.Cache, o Options, m *ast.Module, deps []cache.Key) cache.Key {
	h := cache.NewHasher()
	h.String(keyDomain)
	h.Key(store.Compiler())
	h.Bool(o.CountAllocations)
	h.String(m.LinkPath())
	h.Key(sourceDigest(m))
	h.Int(len(deps))
	for _, d := range deps {
		h.Key(d)
	}
	return h.Sum()
}

// probeTask makes the module's summary and key and reads its entry, once the module's front is done and the summaries of
// the modules it depends on are made, which is all the key holds. A hit's generate does not run, and its check runs only
// if some module that is not a hit reaches it and reads its check results, which is what a module that declares a
// generic, an exported constant or a field's default lets another do: such a hit's check waits for the demand that says
// whether one does. Nothing that can report an error is skipped for a module that has one: only a module that compiled
// cleanly has an entry, and its key says that nothing it reads has changed.
func (p *modulePhase) probeTask(id ast.ModuleID, edges par.Edges) {
	if p.failed.before(stageBodies) {
		return
	}
	m, deps := p.proj.Modules[id], p.results[id].Deps
	shown := make([]cache.Key, len(deps))
	for i, d := range deps {
		shown[i] = p.summaries[d.Module].digest
	}
	own := ownSummary(m)
	p.summaries[id] = moduleSummary{digest: foldSummary(own.digest, shown), readThrough: own.readThrough}
	p.keys[id] = moduleKey(p.options.Cache, p.options, m, shown)
	c := p.probe(id)
	p.cached[id] = c
	if c == nil {
		return
	}
	p.units[id+1] = c.unit
	p.outputs[id] = moduleOutput{usesArrays: c.usesArrays, usesBytes: c.usesBytes, usesOs: c.usesOs, irText: c.irText}
	if own.readThrough {
		edges.Add(p.layout.task(id, taskCheck), p.layout.task(id, taskDemand))
	}
}

// findDependents makes the module graph once every module is resolved, so that each module's demand knows the modules
// that depend on it and waits for what they found. Which modules depend on a module is only known once every module that
// could name it is resolved, so this is the one join of every module in the phase, and only the check of a hit that
// shows its check results waits for it.
func (p *modulePhase) findDependents(edges par.Edges) {
	p.graph = project.NewGraph(p.results)
	for i, dependents := range p.graph.Dependents {
		if len(dependents) == 0 {
			continue
		}
		needs := make([]int, 0, 2*len(dependents))
		for _, d := range dependents {
			needs = append(needs, p.layout.task(d, taskProbe), p.layout.task(d, taskDemand))
		}
		edges.Add(p.layout.task(ast.ModuleID(i), taskDemand), needs...)
	}
}

// demand is whether a miss reaches the module: a module that depends on it is a miss, or a miss reaches that one. A miss
// reads the check results of a module in its whole closure, not only of the modules it names, because it can instantiate
// a generic that a signature of a module it names leads to, and a hit that shows none is not checked however many misses
// reach it.
func (p *modulePhase) demand(id ast.ModuleID) {
	if p.failed.before(stageBodies) {
		return
	}
	for _, d := range p.graph.Dependents[id] {
		if p.cached[d] == nil || p.missReaches[d] {
			p.missReaches[id] = true
			return
		}
	}
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

// hit is whether the module's code came from the cache.
func (p *modulePhase) hit(id ast.ModuleID) bool { return p.cached != nil && p.cached[id] != nil }

// needsCheck is whether the module's check has to run: always without a cache, and with one for a miss and for a hit that
// a miss reaches and that shows its check results to the modules that reach it (its summary says so).
func (p *modulePhase) needsCheck(id ast.ModuleID) bool {
	if p.needCheck == nil {
		return true
	}
	p.needCheck[id] = p.cached[id] == nil || (p.summaries[id].readThrough && p.missReaches[id])
	return p.needCheck[id]
}

// remember stores the module's code and what the tail reads of its output. It is called only for a module with no
// refusal and no error, so every entry is one that a later compile may use.
func (p *modulePhase) remember(id ast.ModuleID, unit *backend.CodeUnit, out *moduleOutput) {
	if p.keys == nil {
		return
	}
	entry := cachedModule{unit: unit, usesArrays: out.usesArrays, usesBytes: out.usesBytes, usesOs: out.usesOs, hasIr: p.options.EmitIr, irText: out.irText}
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
