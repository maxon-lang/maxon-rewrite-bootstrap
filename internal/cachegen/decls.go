package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// declKind is what a declaration of a type is.
type declKind int

const (
	recordDecl declKind = iota
	unionDecl
	enumDecl
	aliasDecl
)

// field is a field of a record or a part of a union case's payload: its name and its type as the source writes it.
type field struct {
	name, typ string
}

// unionCase is a case of a union, with its payload; an enum's case has none.
type unionCase struct {
	name    string
	payload []field
}

// decl is a type declaration of a module of self/, as the generator reads it from the source.
type decl struct {
	module, name string
	kind         declKind
	params       []string
	fields       []field
	cases        []unionCase
	target       string
	file         string
}

var (
	typeHeader  = regexp.MustCompile(`^(?:export )?(type|union|enum) (\w+)(?: uses ([\w, ]+?))?(?: implements .*)?$`)
	aliasHeader = regexp.MustCompile(`^(?:export )?typealias (\w+) = (.+)$`)
	fieldLine   = regexp.MustCompile(`^\t(?:export )?(?:var|let) (\w+) as (.+)$`)
	caseLine    = regexp.MustCompile(`^\t(\w+)(?:\((.*)\))?(?: = .*)?$`)
)

// notCases are the words that begin a line of a union or an enum that is no case.
var notCases = map[string]bool{"export": true, "function": true, "static": true, "var": true, "let": true}

// readDecls reads the type declarations of every source file of module, which the layout rules put one per line from the
// first column, each member one tab in, to the `end` of its name.
func readDecls(selfDir, module string) (map[string]*decl, error) {
	files, err := filepath.Glob(filepath.Join(selfDir, module, "*.maxon"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	decls := make(map[string]*decl)
	for _, f := range files {
		if filepath.Base(f) == outputName {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
		for i := 0; i < len(lines); i++ {
			line := lines[i]
			if m := aliasHeader.FindStringSubmatch(line); m != nil {
				if err := add(decls, &decl{module: module, name: m[1], kind: aliasDecl, target: m[2], file: f}); err != nil {
					return nil, err
				}
				continue
			}
			m := typeHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			d := &decl{module: module, name: m[2], file: f}
			switch m[1] {
			case "type":
				d.kind = recordDecl
			case "union":
				d.kind = unionDecl
			case "enum":
				d.kind = enumDecl
			default:
				panic("cachegen: a declaration of kind " + m[1])
			}
			if m[3] != "" {
				for p := range strings.SplitSeq(m[3], ",") {
					d.params = append(d.params, strings.TrimSpace(p))
				}
			}
			end := "end '" + d.name + "'"
			for i++; i < len(lines) && lines[i] != end; i++ {
				member := lines[i]
				switch d.kind {
				case recordDecl:
					if fm := fieldLine.FindStringSubmatch(member); fm != nil {
						d.fields = append(d.fields, field{fm[1], fm[2]})
					}
				case unionDecl, enumDecl:
					cm := caseLine.FindStringSubmatch(member)
					if cm == nil || notCases[cm[1]] {
						continue
					}
					c := unionCase{name: cm[1]}
					if cm[2] != "" {
						for _, part := range splitTop(cm[2]) {
							sp := strings.SplitN(strings.TrimSpace(part), " ", 2)
							if len(sp) != 2 {
								return nil, fmt.Errorf("%s: the payload %q of %s.%s has no type", f, part, d.name, c.name)
							}
							c.payload = append(c.payload, field{sp[0], sp[1]})
						}
					}
					d.cases = append(d.cases, c)
				case aliasDecl:
					panic("cachegen: an alias has no members")
				}
			}
			if i >= len(lines) {
				return nil, fmt.Errorf("%s: %s has no %s", f, d.name, end)
			}
			if err := add(decls, d); err != nil {
				return nil, err
			}
		}
	}
	return decls, nil
}

func add(decls map[string]*decl, d *decl) error {
	if prev, ok := decls[d.name]; ok {
		return fmt.Errorf("%s: %s.%s is declared twice (also in %s)", d.file, d.module, d.name, prev.file)
	}
	decls[d.name] = d
	return nil
}

// splitTop splits s at the commas outside parentheses.
func splitTop(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}
