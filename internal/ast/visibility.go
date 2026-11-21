package ast

import (
	"minimaxon/internal/diag"
	"strings"
)

// Visibility is how far a declaration, field or method can be named from, narrowest first, so a wider one compares
// greater.
type Visibility uint8

const (
	// Private is the declaring module only: no modifier.
	Private Visibility = iota
	// Exported is the tree rooted at the declaring module's parent, as Go's internal/: in a top-level module or the root
	// that is the whole project. It never reaches past the project.
	Exported
	// Public is everyone, programs that use the project as a library included.
	Public
)

const (
	exportWord = "export"
	publicWord = "public"
)

// VisibilityOf is the visibility a modifier as written gives.
func VisibilityOf(modifier string) Visibility {
	switch modifier {
	case "":
		return Private
	case exportWord:
		return Exported
	case publicWord:
		return Public
	}
	panic(diag.Internal("'%s' is not a visibility modifier", modifier))
}

// Word is the visibility as a diagnostic names it, which is Maxon's.
func (v Visibility) Word() string {
	switch v {
	case Private:
		return "unexported"
	case Exported:
		return "exported"
	case Public:
		return "public"
	}
	panic(diag.Internal("visibility %d has no word", v))
}

// Modifier is what a declaration writes to have the visibility.
func (v Visibility) Modifier() string {
	switch v {
	case Private:
		return ""
	case Exported:
		return exportWord
	case Public:
		return publicWord
	}
	panic(diag.Internal("visibility %d has no modifier", v))
}

func (d *DeclBase) Level() Visibility { return VisibilityOf(d.Visibility) }

func (d *DeclBase) IsExported() bool { return d.Level() != Private }

// Visible is whether something that the module owner declares with the visibility v can be named from the module from.
// This is the one place that decides it.
func Visible(v Visibility, owner, from *Module) bool {
	switch v {
	case Public:
		return true
	case Exported:
		return owner.IsStdlib || within(from, parentPath(owner.Path))
	case Private:
		return owner == from
	}
	panic(diag.Internal("visibility %d is not one of the three", v))
}

// parentPath is the path of the module a module's path is under; the root's and a top-level module's is the root's.
func parentPath(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[:i]
	}
	return ""
}

// within is whether m is the module at path or one under it.
func within(m *Module, path string) bool {
	return path == "" || m.Path == path || strings.HasPrefix(m.Path, path+".")
}

// NotVisible is the message for naming, from the module from, something named name that owner declares with the
// visibility v, which Visible refused.
func NotVisible(name string, v Visibility, owner, from *Module) string {
	if v == Private {
		return "'" + name + "' is not exported from " + owner.Describe() + ": mark it 'export' to use it from " + from.Describe()
	}
	return "'" + name + "' is exported only to module " + parentPath(owner.Path) + " and the modules under it: mark it 'public' to use it from " + from.Describe()
}

// Describe names the module in a sentence.
func (m *Module) Describe() string {
	if m.Path == "" && !m.IsStdlib {
		return m.Display()
	}
	return "module " + m.Display()
}
