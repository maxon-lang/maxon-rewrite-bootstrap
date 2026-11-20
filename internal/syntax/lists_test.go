package syntax

import (
	"slices"
	"testing"

	"minimaxon/internal/ast"
)

func TestAListTakesExactlyTheElementsPushedSinceItsMark(t *testing.T) {
	var s listStack[int]
	outer := s.mark()
	s.push(1)
	inner := s.mark()
	s.push(2)
	s.push(3)
	if got := s.take(inner); !slices.Equal(got, []int{2, 3}) || cap(got) != 2 {
		t.Errorf("inner list %v with capacity %d, want [2 3] with capacity 2", got, cap(got))
	}
	s.push(4)
	if got := s.take(outer); !slices.Equal(got, []int{1, 4}) {
		t.Errorf("outer list %v, want [1 4]", got)
	}
	if empty := s.take(s.mark()); empty != nil {
		t.Errorf("a list with no elements is %v, want nil", empty)
	}
}

func TestAScratchParsesTheModuleAfterAFailedOne(t *testing.T) {
	scratch := NewScratch()
	broken := sourceOfText("function f(a Count, b Count) returns Count\n\treturn g(a, b, )\nend 'f'\n")
	_, errs := scratch.Parse(broken, scratch.Lex(broken), 0)
	if len(errs) != 1 {
		t.Fatalf("the broken module has %d errors: %v", len(errs), errs)
	}
	if left := len(scratch.lists.Stmt.items) + len(scratch.lists.ptr_Arg.items); left != 0 {
		t.Errorf("the failed module left %d elements on the stacks", left)
	}
	fine := sourceOfText("function f(a Count, b Count) returns Count\n\treturn g(a, b)\nend 'f'\n")
	program, errs := scratch.Parse(fine, scratch.Lex(fine), 0)
	if len(errs) != 0 || len(program.Decls) != 1 {
		t.Fatalf("the fine module has errors %v and %d declarations", errs, len(program.Decls))
	}
	fn := program.Decls[0].(*ast.FuncDecl)
	if len(fn.Params) != 2 || cap(fn.Params) != 2 || len(fn.Body) != 1 || cap(fn.Body) != 1 {
		t.Errorf("the lists are %d parameters with capacity %d and %d statements with capacity %d",
			len(fn.Params), cap(fn.Params), len(fn.Body), cap(fn.Body))
	}
}
