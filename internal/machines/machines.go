// Package machines lists the instruction sets the backend writes code for, in the order of the targets of
// internal/targetgen/target: the generators of instruction selection (internal/rulegen) and of the machine IR's tables
// (internal/targetgen) read each through internal/isa.
package machines

import (
	"minimaxon/internal/isa"
	"minimaxon/internal/x64gen/x64table"
)

// All are the instruction sets, one per target of the target table.
var All = []isa.ISA{x64table.Table}
