package ast

import "minimaxon/internal/diag"

// DeclRef is a handle to a top-level declaration: its module and its index among that module's Decls. It is what a
// node that names a declaration holds in place of a pointer to it, so a module's data never points into another
// module's.
type DeclRef struct {
	Module  ModuleID
	Ordinal uint32
}

// Ref is the handle of the declaration.
func (d *DeclBase) Ref() DeclRef { return DeclRef{Module: d.Module.ID, Ordinal: d.Ordinal} }

// ownModule marks a binding to a type parameter, which is always one of the module's own.
const ownModule ModuleID = -2

// binding is one thing a node can be bound to, in 8 bytes: a declaration (its module, and its Ordinal plus one) or a
// type parameter (ownModule, and its slot in Resolution.typeParams plus one).
type binding struct {
	module ModuleID
	index  uint32
}

// Resolution is what name resolution decided about the nodes of one module: which declaration each name, struct
// literal and type reference names, and which type parameter a type reference or a constraint binds. It is a table
// indexed by NodeID, owned by the module's result and written only by that module's resolution, so the nodes keep
// only what the author wrote.
//
// About one node in ten is bound, and to few things (a module names a handful of declarations many times), so an
// entry is 4 bytes: a slot in the list of the distinct things the module bound anything to, and slot 0 is nothing,
// as for a name that is a local, a field or a primitive type.
//
// Everything in the table that depends on the project is a ModuleID in that list. A module that is reused from an
// earlier compile keeps its NodeIDs and its slots, and only the module handles in the list need renumbering when the
// project's modules have moved.
type Resolution struct {
	slots []uint32
	// bound lists the distinct bindings; an entry of slots holds the position in it plus one.
	bound []binding
	// typeParams are the module's type parameters in the order resolution entered them: a binding to one holds its slot.
	typeParams []*TypeParam
	// slotOf finds the slot of a binding that is already listed. It is needed only while resolution fills the table.
	slotOf map[binding]uint32
}

// Reset empties the table and sizes it for the nodes the module has made so far.
func (r *Resolution) Reset(nodes int) {
	r.slots = make([]uint32, nodes+1)
	r.bound, r.typeParams = nil, nil
	r.slotOf = map[binding]uint32{}
}

// Finish ends the filling of the table: it can be read from then on, and not written.
func (r *Resolution) Finish() { r.slotOf = nil }

func (r *Resolution) bind(n NodeID, b binding) {
	if int(n) >= len(r.slots) {
		r.slots = append(r.slots, make([]uint32, int(n)+1-len(r.slots))...)
	}
	slot, listed := r.slotOf[b]
	if !listed {
		r.bound = append(r.bound, b)
		slot = uint32(len(r.bound))
		r.slotOf[b] = slot
	}
	r.slots[n] = slot
}

// BindDecl records that the node names the declaration.
func (r *Resolution) BindDecl(n NodeID, d Decl) {
	ref := d.Base().Ref()
	r.bind(n, binding{module: ref.Module, index: ref.Ordinal + 1})
}

// AddTypeParam makes a type parameter bindable and gives its slot.
func (r *Resolution) AddTypeParam(p *TypeParam) int {
	r.typeParams = append(r.typeParams, p)
	return len(r.typeParams) - 1
}

// BindTypeParam records that the node names the type parameter in slot.
func (r *Resolution) BindTypeParam(n NodeID, slot int) {
	r.bind(n, binding{module: ownModule, index: uint32(slot) + 1})
}

// at is what the node is bound to, and whether it is bound at all.
func (r *Resolution) at(n NodeID) (binding, bool) {
	if int(n) >= len(r.slots) {
		panic(diag.Internal("resolution: node %d is not in a table of %d nodes", n, len(r.slots)))
	}
	slot := r.slots[n]
	if slot == 0 {
		return binding{}, false
	}
	return r.bound[slot-1], true
}

// Decl is the declaration the node names, or nil if it names none.
func (r *Resolution) Decl(p *Project, n NodeID) Decl {
	b, bound := r.at(n)
	if !bound || b.module == ownModule {
		return nil
	}
	return p.Decl(DeclRef{Module: b.module, Ordinal: b.index - 1})
}

// TypeParam is the type parameter the node binds, or nil if it binds none.
func (r *Resolution) TypeParam(n NodeID) *TypeParam {
	b, bound := r.at(n)
	if !bound || b.module != ownModule {
		return nil
	}
	return r.typeParams[b.index-1]
}
