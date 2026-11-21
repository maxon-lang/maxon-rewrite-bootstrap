package ast

import "testing"

// linkProject is a project with the root and a module `geometry`, which each declare a type `Counter`, and a generic `Box` in
// the root.
func linkProject() (p *Project, rootCounter, geometryCounter *RecordTy, box *RecordTy) {
	p = NewProject(NewModule("", ".", false))
	geometry := NewModule("geometry", "geometry", false)
	p.Root.Children["geometry"] = geometry
	declare := func(m *Module, name string, params ...string) *RecordTy {
		d := &TypeDecl{}
		d.Name, d.Module = name, m
		for _, param := range params {
			d.TypeParams = append(d.TypeParams, &TypeParam{Name: param})
		}
		t := NewRecordTy(d)
		d.Ty = t
		m.Names[name] = d
		return t
	}
	return p, declare(p.Root, "Counter"), declare(geometry, "Counter"), declare(p.Root, "Box", "T")
}

func TestALinkNameStartsWithTheModuleThatDeclaresIt(t *testing.T) {
	p, rootCounter, geometryCounter, _ := linkProject()
	if got := rootCounter.LinkName(); got != "export::Counter" {
		t.Errorf("the root's type is %q", got)
	}
	if got := geometryCounter.LinkName(); got != "geometry::Counter" {
		t.Errorf("a module's type is %q", got)
	}
	option := p.Builtins.Option
	if got := Instantiate(option, []Ty{p.Builtins.Count}).LinkName(); got != "stdlib::Option with Count=int(0 to 18446744073709551615)" {
		t.Errorf("the standard library's instance is %q", got)
	}
}

// Two types of one name in two modules are two types, and so are the instances of a generic that they are the arguments of:
// the code of one is not the code of the other, so no link may treat them as one.
func TestInstancesAtTypesOfOneNameInTwoModulesHaveTwoLinkNames(t *testing.T) {
	_, rootCounter, geometryCounter, box := linkProject()
	a, b := Instantiate(box, []Ty{rootCounter}), Instantiate(box, []Ty{geometryCounter})
	if a.LinkName() == b.LinkName() {
		t.Errorf("both are %q", a.LinkName())
	}
	generic := &FuncDecl{}
	generic.Name, generic.Module = "swap", box.Decl.Module
	generic.Instances = &InstanceTable{}
	generic.Generics, generic.ReturnType = box.Params, VoidTy
	instA, instB := generic.Instance([]Ty{rootCounter}), generic.Instance([]Ty{geometryCounter})
	if instA.LinkName != "export::swap[export::Counter]" || instB.LinkName != "export::swap[geometry::Counter]" {
		t.Errorf("the instances are %q and %q", instA.LinkName, instB.LinkName)
	}
}

func TestAFunctionAndAMethodOfTheSameNameInTwoModulesHaveTwoLinkNames(t *testing.T) {
	_, rootCounter, geometryCounter, box := linkProject()
	link := func(name string, m *Module, owner NominalTy) string {
		f := &FuncDecl{}
		f.Name, f.Module, f.Owner = name, m, owner
		f.SetLinkName()
		return f.LinkName
	}
	geometry := geometryCounter.Decl.Module
	for want, got := range map[string]string{
		"export::helper":             link("helper", box.Decl.Module, nil),
		"geometry::helper":           link("helper", geometry, nil),
		"export::Counter.inc":        link("inc", box.Decl.Module, rootCounter),
		"geometry::Counter.inc":      link("inc", geometry, geometryCounter),
		"(export::Box with T).make":  link("make", box.Decl.Module, SelfType(box)),
		"(export::Box with T).other": link("other", box.Decl.Module, SelfType(box)),
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// A link name reads back as the type it names, so code that was generated in one compile can ask for its routines in
// another.
func TestALinkNameReadsBackAsTheTypeItNames(t *testing.T) {
	p, rootCounter, geometryCounter, box := linkProject()
	b := p.Builtins
	score := &IntTy{Alias: "Score", Min: I64Min, Max: I64Max}
	nested := Instantiate(box, []Ty{Instantiate(b.Array, []Ty{geometryCounter})})
	for _, ty := range []Ty{
		StringTy, BoolTy, b.Count, rootCounter, geometryCounter,
		Instantiate(box, []Ty{rootCounter}), nested, Instantiate(b.Map, []Ty{StringTy, nested}),
		Instantiate(b.Option, []Ty{b.HandleOf(rootCounter)}),
	} {
		got, err := ParseLinkType(p, ty.LinkName())
		if err != nil {
			t.Errorf("%s: %v", ty.LinkName(), err)
			continue
		}
		if got != ty {
			t.Errorf("%s reads back as a different type, %s", ty.LinkName(), got.LinkName())
		}
	}
	// An integer type is made again, with what its name says, so a type that holds one is another instance with the same name.
	got, err := ParseLinkType(p, score.LinkName())
	if err != nil {
		t.Fatal(err)
	}
	if read, ok := got.(*IntTy); !ok || read.Alias != "Score" || read.Min != score.Min || read.Max != score.Max {
		t.Errorf("%s reads back as %#v", score.LinkName(), got)
	}
	for _, ty := range []Ty{Anonymous64, Instantiate(box, []Ty{score}), Instantiate(b.Array, []Ty{Instantiate(box, []Ty{Anonymous64})})} {
		got, err := ParseLinkType(p, ty.LinkName())
		if err != nil || got.LinkName() != ty.LinkName() {
			t.Errorf("%s reads back as %v (%v)", ty.LinkName(), got, err)
		}
	}
}

func TestAnUnknownLinkNameIsAnError(t *testing.T) {
	p, _, _, _ := linkProject()
	for _, name := range []string{
		"", "export::", "export::Missing", "nowhere::Counter", "geometry::Box", "export::Box with",
		"export::Box with (export::Counter", "export::Box with (export::Counter, export::Counter)", "export::Counter with String",
		"export::Counter trailing", "int(1 to", "int(x to 2)", "Score=int(1 to 2", "stdlib::Array with ()", "mystery",
		"export::Box with (String,String)",
	} {
		if got, err := ParseLinkType(p, name); err == nil {
			t.Errorf("%q read as %v", name, got.LinkName())
		}
	}
}
