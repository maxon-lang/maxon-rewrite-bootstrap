package ir

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
)

func TestFinishDropsUnreachableBlocksAndNumbersTheRest(t *testing.T) {
	decl := &ast.FuncDecl{Name: "f"}
	var b Builder
	b.Start(decl, "", 0)
	entry := b.NewBlock()
	dead := b.NewBlock()
	exit := b.NewBlock()
	v := b.F.NewReg(KindScalar)
	b.Emit(entry, Const(v, 7))
	b.Emit(dead, Const(v, 8))
	b.Terminate(entry, Jump(exit))
	b.Terminate(dead, Trap())
	b.Terminate(exit, Ret(v))
	f := b.Finish(nil)

	var sb strings.Builder
	PrintIr(f, &sb)
	want := "function f params 0\nb0:\n  %0 = const 7\n  jump b1\nb1:\n  ret %0\n\n"
	if got := sb.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
