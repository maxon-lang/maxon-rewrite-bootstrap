package ir

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
)

func TestRemoveUnreachableAndPrint(t *testing.T) {
	decl := &ast.FuncDecl{Name: "f"}
	f := &IrFunction{Decl: decl}
	entry := f.NewBlock()
	dead := f.NewBlock()
	exit := f.NewBlock()
	v := f.NewReg(KindScalar)
	entry.Instrs = append(entry.Instrs, &ConstInstr{Dst: v, Value: 7})
	entry.Term = &JumpTerm{Target: exit}
	dead.Term = &TrapTerm{}
	exit.Term = &RetTerm{Value: v}

	f.RemoveUnreachable()

	var sb strings.Builder
	PrintIr(f, &sb)
	want := "function f params 0\nb0:\n  %0 = const 7\n  jump b1\nb1:\n  ret %0\n\n"
	if got := sb.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
