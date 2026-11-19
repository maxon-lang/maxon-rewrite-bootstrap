// Command astgen generates internal/ast/nodes_gen.go from internal/ast/nodes.spec, which describes the AST nodes,
// their enums and the closed sets the phases dispatch over. `go generate ./internal/ast` runs it in that directory.
package main

import (
	"fmt"
	"go/format"
	"os"
	"strings"
)

const (
	specFile   = "nodes.spec"
	outputFile = "nodes_gen.go"
)

func main() {
	src, err := os.ReadFile(specFile)
	if err != nil {
		fail(err)
	}
	out, err := generate(specFile, src)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(outputFile, out, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "astgen:", err)
	os.Exit(1)
}

type set struct {
	name    string
	noun    string
	members []*node
}

type field struct {
	name string
	typ  string
	// gap keeps a blank line before the field, to group fields the way the spec does.
	gap bool
}

type node struct {
	name   string
	fields []field
}

type enum struct {
	name   string
	values []string
}

// decl is one top-level declaration of the spec, kept in spec order so the output follows it.
type decl interface {
	declName() string
}

func (n *node) declName() string { return n.name }
func (e *enum) declName() string { return e.name }

type spec struct {
	sets  []*set
	decls []decl
}

type specError struct {
	file string
	line int
	msg  string
}

func (e *specError) Error() string { return fmt.Sprintf("%s:%d: %s", e.file, e.line, e.msg) }

func generate(file string, src []byte) ([]byte, error) {
	s, err := parse(file, string(src))
	if err != nil {
		return nil, err
	}
	out, err := format.Source(emit(s))
	if err != nil {
		return nil, fmt.Errorf("generated code does not parse: %w", err)
	}
	return out, nil
}

func parse(file, src string) (*spec, error) {
	s := &spec{}
	sets := map[string]*set{}
	names := map[string]bool{}
	var curNode *node
	var curEnum *enum
	pendingGap := false
	errAt := func(line int, format string, args ...any) error {
		return &specError{file, line, fmt.Sprintf(format, args...)}
	}
	declare := func(line int, name string) error {
		if names[name] {
			return errAt(line, "'%s' is declared twice", name)
		}
		names[name] = true
		return nil
	}

	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "" {
			pendingGap = curNode != nil && len(curNode.fields) > 0
			continue
		}
		words := strings.Fields(trimmed)
		indented := raw[0] == ' ' || raw[0] == '\t'

		if indented {
			switch {
			case curNode != nil:
				f := field{gap: pendingGap}
				switch len(words) {
				case 1:
					f.typ = words[0]
				case 2:
					f.name, f.typ = words[0], words[1]
				default:
					return nil, errAt(line, "a field is '<Name> <Type>' or a bare type to embed")
				}
				curNode.fields = append(curNode.fields, f)
			case curEnum != nil:
				if len(words) != 1 {
					return nil, errAt(line, "an enum value is one name")
				}
				if err := declare(line, words[0]); err != nil {
					return nil, err
				}
				curEnum.values = append(curEnum.values, words[0])
			default:
				return nil, errAt(line, "an indented line belongs to a node or an enum")
			}
			pendingGap = false
			continue
		}

		curNode, curEnum, pendingGap = nil, nil, false
		switch words[0] {
		case "set":
			if len(words) < 3 {
				return nil, errAt(line, "a set is 'set <Interface> <noun>'")
			}
			if err := declare(line, words[1]); err != nil {
				return nil, err
			}
			st := &set{name: words[1], noun: strings.Join(words[2:], " ")}
			sets[st.name] = st
			s.sets = append(s.sets, st)
		case "enum":
			if len(words) != 2 {
				return nil, errAt(line, "an enum is 'enum <Name>'")
			}
			if err := declare(line, words[1]); err != nil {
				return nil, err
			}
			curEnum = &enum{name: words[1]}
			s.decls = append(s.decls, curEnum)
		case "node":
			if len(words) < 2 || (len(words) > 2 && (words[2] != "in" || len(words) == 3)) {
				return nil, errAt(line, "a node is 'node <Name>' or 'node <Name> in <Set>...'")
			}
			if err := declare(line, words[1]); err != nil {
				return nil, err
			}
			curNode = &node{name: words[1]}
			s.decls = append(s.decls, curNode)
			if len(words) > 2 {
				for _, setName := range words[3:] {
					st, ok := sets[setName]
					if !ok {
						return nil, errAt(line, "'%s' is not a set declared above", setName)
					}
					st.members = append(st.members, curNode)
				}
			}
		default:
			return nil, errAt(line, "expected 'set', 'enum' or 'node', found '%s'", words[0])
		}
	}

	for _, st := range s.sets {
		if len(st.members) == 0 {
			return nil, fmt.Errorf("%s: set '%s' has no members", file, st.name)
		}
	}
	for _, d := range s.decls {
		if e, ok := d.(*enum); ok && len(e.values) == 0 {
			return nil, fmt.Errorf("%s: enum '%s' has no values", file, e.name)
		}
	}
	return s, nil
}

type flavour struct {
	suffix     string
	typeParams string
	typeArgs   string
	param      string
	arg        string
	result     string
}

// The three ways a phase walks a set: for effect, computing a result, or threading a state through and returning one.
var flavours = []flavour{
	{suffix: "Visitor"},
	{suffix: "ResultVisitor", typeParams: "[R any]", typeArgs: "[R]", result: " R"},
	{suffix: "StateVisitor", typeParams: "[S, R any]", typeArgs: "[S, R]", param: ", s S", arg: ", s", result: " R"},
}

func emit(s *spec) []byte {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("// Code generated by internal/astgen from %s; DO NOT EDIT.\n\n", specFile)
	w("package ast\n\nimport \"minimaxon/internal/diag\"\n\n")

	for _, d := range s.decls {
		switch d := d.(type) {
		case *enum:
			w("type %s int\n\nconst (\n", d.name)
			for i, v := range d.values {
				if i == 0 {
					w("\t%s %s = iota\n", v, d.name)
				} else {
					w("\t%s\n", v)
				}
			}
			w(")\n\n")
		case *node:
			w("type %s struct {\n", d.name)
			for _, f := range d.fields {
				if f.gap {
					w("\n")
				}
				w("\t%s %s\n", f.name, f.typ)
			}
			w("}\n\n")
		default:
			panic(fmt.Sprintf("astgen: unhandled declaration %T", d))
		}
	}

	for _, st := range s.sets {
		w("var (\n")
		for _, n := range st.members {
			w("\t_ %s = (*%s)(nil)\n", st.name, n.name)
		}
		w(")\n\n")

		for _, f := range flavours {
			iface := st.name + f.suffix
			w("type %s%s interface {\n", iface, f.typeParams)
			for _, n := range st.members {
				w("\tVisit%s(n *%s%s)%s\n", n.name, n.name, f.param, f.result)
			}
			w("}\n\n")

			fn := "Visit" + st.name + strings.TrimSuffix(f.suffix, "Visitor")
			w("func %s%s(v %s%s, n %s%s)%s {\n", fn, f.typeParams, iface, f.typeArgs, st.name, f.param, f.result)
			w("\tswitch n := n.(type) {\n")
			for _, m := range st.members {
				ret := "return "
				if f.result == "" {
					ret = ""
				}
				w("\tcase *%s:\n\t\t%sv.Visit%s(n%s)\n", m.name, ret, m.name, f.arg)
				if f.result == "" {
					w("\t\treturn\n")
				}
			}
			w("\t}\n")
			w("\tpanic(diag.Internal(\"unhandled %s %%T\", n))\n}\n\n", st.noun)
		}
	}
	return []byte(b.String())
}
