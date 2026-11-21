package main

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/driver"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The rename command rewrites built-in types and calls of built-in methods in programs by what the checker resolved each
// name to, never by its text: `append` on an array is renamed by a rule for Array.append, and `append` on a list is left
// alone. A rule is one of
//
//	old=New                  a built-in type, wherever a type or an expression names it
//	Owner.old=new            a built-in method of Owner (a type of the table, or String), at every call of it
//	.old=new                 the deep copy that every type has, whose name is old (.copy=clone renamed it)
//	Owner.old=new(template)  the call's name and arguments, written again from the template, in which {param} is the
//	                         text of the argument for that parameter of the old signature
//	Owner.old=...{call}...   the whole call, receiver and all, written again from the template, in parentheses where it is
//	                         an operand: Array.pop=try {call} otherwise panic("empty"); {self} in it is the text of the
//	                         receiver, and {param} the argument's: String.byteAt={self}[{index}]
//	Owner.old.m=...{call}... as the one before, for a call followed by '.m()', which the template replaces too
//	old()=template           a built-in free function: the whole call written again from the template, {param} as above,
//	                         as when the table makes one a static method: readFile()=File.readText({path})
//	old().m=template         as the one before, for a call followed by '.m()', which the template replaces too
//	Owner.old=?              lists the calls instead of rewriting them
//
// A program is a file or a project directory as check takes it; a file of cases ("// case: name" lines) is checked case by
// case, each after the --prelude files, as the unit tests read it. --rules=FILE reads rules from a file, one to a line.
// What the checker did not reach, because the program stops at an error first, is listed so it can be renamed by hand.

const (
	preludeFlagPrefix = "--prelude="
	rulesFlagPrefix   = "--rules="
	caseMarker        = "// case: "
	copyRuleOwner     = ""
	// listRule, as what a rule writes, lists the calls it matches instead of renaming them.
	listRule = "?"
	// callPlaceholder, in a template, is the text of the whole call.
	callPlaceholder = "{call}"
	// selfPlaceholder, in a template, is the text of the receiver the method is called on.
	selfPlaceholder = "{self}"
)

// renameRule is one rule: what it renames, and what it writes instead.
type renameRule struct {
	owner, old string
	isType     bool
	newName    string
	// template, when not empty, replaces the name and the arguments of the call, or with {call} in it the whole call.
	template string
	// suffix, when not empty, is a method with no arguments that the call must be followed by, as in
	// Array.pop.unwrap=...: the rule then replaces the call of it too.
	suffix string
	// free is whether old is a built-in free function, whose whole call the template replaces.
	free bool
}

// freeCallMark ends the name of a free function in a rule.
const freeCallMark = "()"

func parseRenameRule(s string) (renameRule, bool) {
	from, to, ok := strings.Cut(s, "=")
	if !ok || from == "" || to == "" {
		return renameRule{}, false
	}
	r := renameRule{newName: to}
	if name, _, hasArgs := strings.Cut(to, "("); hasArgs || strings.Contains(to, "{") {
		r.newName, r.template = name, to
	}
	if name, rest, isFree := strings.Cut(from, freeCallMark); isFree && !strings.Contains(name, ".") {
		r.free, r.old = true, name
		if rest != "" {
			if !strings.HasPrefix(rest, ".") {
				return renameRule{}, false
			}
			r.suffix = rest[1:]
		}
		return r, name != "" && r.template != ""
	}
	owner, old, isMethod := strings.Cut(from, ".")
	if !isMethod {
		r.isType, r.old = true, from
		return r, r.template == ""
	}
	r.owner = owner
	r.old, r.suffix, _ = strings.Cut(old, ".")
	return r, r.old != ""
}

// renameEdit replaces text[begin:end] of a file. A template edit writes its parts, each literal text or the text of an
// argument, which is itself written with the edits inside it.
type renameEdit struct {
	begin, end int
	text       string
	parts      []templatePart
}

type templatePart struct {
	literal    string
	begin, end int // the argument's text, when literal is empty and end > begin
	isArg      bool
}

// renameSite is a place in a file that the rules rename.
type renameSite struct {
	file string
	edit renameEdit
	// missed, when not empty, is a name the rules would rename at edit.begin that could not be placed.
	missed string
}

func renameCommand(args []string) int {
	var rules []renameRule
	var preludes, programs, ruleTexts []string
	for _, a := range args {
		if strings.HasPrefix(a, rulesFlagPrefix) {
			data, err := os.ReadFile(a[len(rulesFlagPrefix):])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			for line := range strings.Lines(string(data)) {
				if line = strings.TrimSpace(line); line != "" {
					ruleTexts = append(ruleTexts, line)
				}
			}
			continue
		}
		if strings.Contains(a, "=") && !strings.HasPrefix(a, preludeFlagPrefix) {
			ruleTexts = append(ruleTexts, a)
			continue
		}
		if strings.HasPrefix(a, preludeFlagPrefix) {
			preludes = append(preludes, strings.Split(a[len(preludeFlagPrefix):], ",")...)
		} else {
			programs = append(programs, a)
		}
	}
	for _, text := range ruleTexts {
		r, ok := parseRenameRule(text)
		if !ok {
			fmt.Fprintf(os.Stderr, "rename: %q is no rule\n", text)
			return usageExitCode
		}
		rules = append(rules, r)
	}
	if len(rules) == 0 || len(programs) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return usageExitCode
	}
	r := &renamer{rules: rules, edits: map[string]map[int]renameEdit{}}
	for _, p := range programs {
		if err := r.program(p, preludes); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	files := make([]string, 0, len(r.edits))
	for f := range r.edits {
		files = append(files, f)
	}
	sort.Strings(files)
	changed := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		out := renderEdits(string(data), r.edits[f])
		if out != string(data) {
			if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			changed++
		}
	}
	fmt.Printf("renamed in %d files; %d places were not checked and are listed above\n", changed, r.missed)
	return 0
}

type renamer struct {
	rules []renameRule
	// edits are by file and by the offset where each begins, so a place that several cases check is renamed once.
	edits  map[string]map[int]renameEdit
	missed int
}

// segment maps a run of the checked file's text to where it came from.
type segment struct {
	file          string
	begin, length int // in the checked text
	origin        int // in file
}

func (r *renamer) program(path string, preludes []string) error {
	data, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if statErr != nil {
		return statErr
	}
	if info.IsDir() || (err == nil && !strings.Contains(string(data), caseMarker) && len(preludes) == 0) {
		return r.check(path, nil)
	}
	if err != nil {
		return err
	}
	var head strings.Builder
	var segs []segment
	for _, p := range preludes {
		pd, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		segs = append(segs, segment{file: p, begin: head.Len(), length: len(pd)})
		head.Write(pd)
		head.WriteString("\n")
	}
	text := string(data)
	for _, c := range splitCases(text) {
		var src strings.Builder
		src.WriteString(head.String())
		caseSegs := append(append([]segment(nil), segs...), segment{file: path, begin: src.Len(), length: c[1] - c[0], origin: c[0]})
		src.WriteString(text[c[0]:c[1]])
		dir, err := os.MkdirTemp("", "minimaxon-rename")
		if err != nil {
			return err
		}
		tmp := filepath.Join(dir, "case.maxon")
		if err := os.WriteFile(tmp, []byte(src.String()), 0o644); err != nil {
			return err
		}
		err = r.check(tmp, caseSegs)
		os.RemoveAll(dir)
		if err != nil {
			return err
		}
	}
	return nil
}

// splitCases is where each case of a file of cases is in its text, or the whole text for a file of one program.
func splitCases(text string) [][2]int {
	var starts []int
	for at := 0; at < len(text); {
		if strings.HasPrefix(text[at:], caseMarker) {
			starts = append(starts, at)
		}
		next := strings.IndexByte(text[at:], '\n')
		if next < 0 {
			break
		}
		at += next + 1
	}
	if len(starts) == 0 {
		return [][2]int{{0, len(text)}}
	}
	out := make([][2]int, len(starts))
	for i, s := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		// The blank lines between cases belong to neither, as srctest.Cases reads them.
		for end-1 > s && text[end-1] == '\n' && text[end-2] == '\n' {
			end--
		}
		out[i] = [2]int{s, end}
	}
	return out
}

// check compiles the program as far as it goes and notes the edits of what it resolved. segs, when not nil, map the
// one file the program is back to the files it was made of.
func (r *renamer) check(path string, segs []segment) error {
	var found []renameSite
	_ = driver.Compile(path, "", driver.Options{Jobs: jobs, Inspect: func(proj *ast.Project, results []ast.ModuleResult) {
		for i, m := range proj.Modules {
			if m.Tree == nil {
				continue
			}
			found = append(found, r.module(proj, m, &results[i])...)
		}
	}})
	for _, s := range found {
		file, edit := s.file, s.edit
		if segs != nil {
			var ok bool
			file, edit, ok = mapEdit(segs, edit)
			if !ok {
				continue
			}
		}
		if s.missed != "" {
			r.report(file, edit.begin, s.missed)
			continue
		}
		if r.edits[file] == nil {
			r.edits[file] = map[int]renameEdit{}
		}
		r.edits[file][edit.begin] = edit
	}
	return nil
}

func mapEdit(segs []segment, e renameEdit) (string, renameEdit, bool) {
	for _, s := range segs {
		if e.begin >= s.begin && e.end <= s.begin+s.length {
			shift := s.origin - s.begin
			e.begin += shift
			e.end += shift
			for i := range e.parts {
				if e.parts[i].isArg {
					e.parts[i].begin += shift
					e.parts[i].end += shift
				}
			}
			return s.file, e, true
		}
	}
	return "", e, false
}

// moduleText reads the files of a module, by file.
type moduleText struct {
	m     *ast.Module
	texts map[string]string
}

func (t *moduleText) at(off int) (file string, text string, local int) {
	span := t.m.Positions.Files()[t.m.Positions.FileOf(off)]
	if _, ok := t.texts[span.File]; !ok {
		data, err := os.ReadFile(span.File)
		if err != nil {
			panic(diag.Internal("rename: cannot read %s: %v", span.File, err))
		}
		t.texts[span.File] = string(data)
	}
	return span.File, t.texts[span.File], off - span.Begin
}

func (r *renamer) module(proj *ast.Project, m *ast.Module, res *ast.ModuleResult) []renameSite {
	text := &moduleText{m: m, texts: map[string]string{}}
	var sites []renameSite
	tree := m.Tree
	if !res.Typing.Numbered() {
		// The compile stopped before the declare pass, so nothing is resolved: the module's names are listed.
		for id := 1; id < len(tree.Records); id++ {
			ref := ast.Ref{T: tree, ID: ast.NodeID(id)}
			for _, rule := range r.rules {
				if name := nodeName(ref); name != "" && name == rule.old {
					file, src, at := text.at(int(ref.Position().Off))
					sites = append(sites, r.miss(file, src, at, name)...)
				}
			}
		}
		return sites
	}
	for id := 1; id < len(tree.Records); id++ {
		ref := ast.Ref{T: tree, ID: ast.NodeID(id)}
		switch tree.Records[id].Kind {
		case ast.KindTypeRef:
			tr := ast.TypeRef{Ref: ref}
			sites = append(sites, r.typeName(proj, res, text, ref, tr.Name(), tr.Qualifier().Len())...)
		case ast.KindNameExpr:
			n := ast.NameExpr{Ref: ref}
			sites = append(sites, r.typeName(proj, res, text, ref, n.Name(), 0)...)
		case ast.KindCallExpr:
			sites = append(sites, r.call(proj, res, text, ast.CallExpr{Ref: ref})...)
		}
	}
	return sites
}

// nodeName is the name a type reference, a name or a member step writes, or "".
func nodeName(ref ast.Ref) string {
	switch ref.Kind() {
	case ast.KindTypeRef:
		return ast.TypeRef{Ref: ref}.Name()
	case ast.KindNameExpr:
		return ast.NameExpr{Ref: ref}.Name()
	case ast.KindMemberExpr:
		return ast.MemberExpr{Ref: ref}.Name()
	}
	return ""
}

func (r *renamer) typeName(proj *ast.Project, res *ast.ModuleResult, text *moduleText, ref ast.Ref, name string, qualifiers int) []renameSite {
	for _, rule := range r.rules {
		if !rule.isType || rule.old != name {
			continue
		}
		d := res.Resolution.Decl(proj, ref.ID)
		if d == nil || d.Base().Module != proj.Stdlib {
			return nil
		}
		file, src, at := text.at(int(ref.Position().Off))
		for range qualifiers {
			dot := strings.IndexByte(src[at:], '.')
			if dot < 0 {
				return r.miss(file, src, at, name)
			}
			at += dot + 1
		}
		if !hasName(src, at, name) {
			return r.miss(file, src, at, name)
		}
		return []renameSite{{file: file, edit: renameEdit{begin: at, end: at + len(name), text: rule.newName}}}
	}
	return nil
}

func (r *renamer) miss(file, src string, at int, name string) []renameSite {
	return []renameSite{{file: file, edit: renameEdit{begin: at, end: at}, missed: name}}
}

// report lists a place that was not renamed.
func (r *renamer) report(file string, at int, name string) {
	data, err := os.ReadFile(file)
	if err != nil {
		panic(diag.Internal("rename: cannot read %s: %v", file, err))
	}
	src := string(data)
	line := 1 + strings.Count(src[:at], "\n")
	col := at - strings.LastIndexByte(src[:at], '\n')
	fmt.Printf("unplaced %s:%d:%d: '%s'\n", file, line, col, name)
	r.missed++
}

// ruleFor is the rule that renames a call of the method name, as the checker resolved the call, or nil.
func (r *renamer) rulesFor(proj *ast.Project, info *ast.CallInfo, name string) []*renameRule {
	var out []*renameRule
	for i := range r.rules {
		rule := &r.rules[i]
		if rule.isType || rule.free || rule.old != name {
			continue
		}
		if rule.owner == copyRuleOwner {
			if info.Kind == ast.CallCopy {
				out = append(out, rule)
			}
			continue
		}
		if info.Target == nil || info.Target.Intrinsic == ast.NotIntrinsic {
			continue
		}
		if f := builtinMethod(proj, rule.owner, name); f != nil && f.Intrinsic == info.Target.Intrinsic {
			out = append(out, rule)
		}
	}
	return out
}

// builtinMethod is the method name of the built-in type that a rule names, or of String.
func builtinMethod(proj *ast.Project, owner, name string) *ast.FuncDecl {
	if owner == "String" {
		return proj.Builtins.StringMethods[name]
	}
	var ty ast.Ty
	switch d := proj.Stdlib.Names[owner].(type) {
	case *ast.TypeDecl:
		ty = d.Ty
	case *ast.UnionDecl:
		ty = d.Ty
	}
	nt, ok := ty.(ast.NominalTy)
	if !ok {
		panic(diag.Internal("rename: %s is no built-in type with methods", owner))
	}
	return nt.MethodTable()[name]
}

func (r *renamer) call(proj *ast.Project, res *ast.ModuleResult, text *moduleText, call ast.CallExpr) []renameSite {
	info := res.Typing.Call(call)
	if n, isName := call.Callee().AsNameExpr(); isName {
		return r.freeCall(proj, text, call, info, n)
	}
	m, ok := call.Callee().AsMemberExpr()
	if !ok {
		return nil
	}
	candidates := r.rulesFor(proj, info, m.Name())
	if len(candidates) == 0 {
		if info.Kind == ast.CallUnresolved {
			for _, other := range r.rules {
				if !other.isType && !other.free && other.old == m.Name() {
					file, src, at := text.at(int(call.Position().Off))
					return r.miss(file, src, at, "."+m.Name())
				}
			}
		}
		return nil
	}
	leaf, steps := chainLeaf(m.Obj())
	file, src, start := text.at(int(leaf.Position().Off))
	if candidates[0].newName == listRule {
		end := strings.IndexByte(src[start:], '\n')
		return r.miss(file, src, start, "list "+m.Name()+": "+src[start:start+end])
	}
	names := chainNames(src, start)
	if n, isName := leaf.AsNameExpr(); isName && !hasName(src, start, n.Name()) {
		// Resolution made the leaf of a qualified name, 'module.Name', which starts at the module.
		for j := range names {
			if hasName(src, names[j], n.Name()) {
				steps += j + 1
				break
			}
		}
	}
	if steps >= len(names) || !hasName(src, names[steps], m.Name()) {
		return r.miss(file, src, start, "."+m.Name())
	}
	at := names[steps]
	end := at + len(m.Name())
	if end >= len(src) || src[end] != '(' {
		return r.miss(file, src, at, "."+m.Name())
	}
	close := skipGroup(src, end)
	var rule *renameRule
	for _, c := range candidates {
		if c.suffix == "" || strings.HasPrefix(src[close:], "."+c.suffix+"()") {
			rule = c
			break
		}
	}
	switch {
	case rule == nil:
		return nil
	case rule.template == "":
		return []renameSite{{file: file, edit: renameEdit{begin: at, end: end, text: rule.newName}}}
	}
	argText, ok := r.argTexts(src, call, info, end, close)
	if !ok {
		return r.miss(file, src, at, "."+m.Name())
	}
	if strings.Contains(rule.template, callPlaceholder) || strings.Contains(rule.template, selfPlaceholder) {
		spanEnd := close
		if rule.suffix != "" {
			spanEnd += len("." + rule.suffix + "()")
		}
		argText[placeholderName(callPlaceholder)] = [2]int{start, close}
		// The receiver is what the chain writes before the '.' of the method.
		argText[placeholderName(selfPlaceholder)] = [2]int{start, at - 1}
		parts, ok := templateParts(rule.template, argText)
		if !ok {
			return r.miss(file, src, at, "."+m.Name())
		}
		if needsParens(src, start, spanEnd) {
			parts = append(append([]templatePart{{literal: "("}}, parts...), templatePart{literal: ")"})
		}
		return []renameSite{{file: file, edit: renameEdit{begin: start, end: spanEnd, parts: parts}}}
	}
	parts, ok := templateParts(rule.template, argText)
	if !ok {
		return r.miss(file, src, at, "."+m.Name())
	}
	return []renameSite{{file: file, edit: renameEdit{begin: at, end: close, parts: parts}}}
}

func placeholderName(p string) string { return p[1 : len(p)-1] }

// freeCall rewrites a call of a built-in free function that a rule names: the whole call, and the method after it that
// the rule's suffix names.
func (r *renamer) freeCall(proj *ast.Project, text *moduleText, call ast.CallExpr, info *ast.CallInfo, n ast.NameExpr) []renameSite {
	var candidates []*renameRule
	for i := range r.rules {
		rule := &r.rules[i]
		if rule.free && rule.old == n.Name() {
			candidates = append(candidates, rule)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	file, src, at := text.at(int(n.Position().Off))
	if info.Target == nil || info.Target.Module != proj.Stdlib || info.Target.Intrinsic == ast.NotIntrinsic {
		if info.Kind == ast.CallUnresolved {
			return r.miss(file, src, at, n.Name()+freeCallMark)
		}
		return nil
	}
	end := at + len(n.Name())
	if !hasName(src, at, n.Name()) || end >= len(src) || src[end] != '(' {
		return r.miss(file, src, at, n.Name()+freeCallMark)
	}
	close := skipGroup(src, end)
	for _, rule := range candidates {
		spanEnd := close
		if rule.suffix != "" {
			if !strings.HasPrefix(src[close:], "."+rule.suffix+"()") {
				continue
			}
			spanEnd += len("." + rule.suffix + "()")
		}
		var parts []templatePart
		argText, ok := r.argTexts(src, call, info, end, close)
		if ok {
			parts, ok = templateParts(rule.template, argText)
		}
		if !ok {
			return r.miss(file, src, at, n.Name()+freeCallMark)
		}
		if needsParens(src, at, spanEnd) && !isPostfix(rule.template) {
			parts = append(append([]templatePart{{literal: "("}}, parts...), templatePart{literal: ")"})
		}
		return []renameSite{{file: file, edit: renameEdit{begin: at, end: spanEnd, parts: parts}}}
	}
	return nil
}

// argTexts are the spans of the arguments of a call whose parentheses are at open and before close, by the name of the
// parameter of the old signature each is given to.
func (r *renamer) argTexts(src string, call ast.CallExpr, info *ast.CallInfo, open, close int) (map[string][2]int, bool) {
	args := splitArgs(src, open+1, close-1)
	written := call.Args()
	if len(args) != written.Len() {
		return nil, false
	}
	argText := map[string][2]int{}
	for i, spec := range info.Target.Pass.Specs {
		if i >= len(info.Ordered) {
			break
		}
		for j, a := range written.All() {
			if a.Value().ID == info.Ordered[i].ID {
				b, e := args[j][0], args[j][1]
				if a.Label() != "" {
					b = labelEnd(src, b)
				}
				argText[spec.Name] = [2]int{b, e}
			}
		}
	}
	return argText, true
}

// needsParens is whether an expression written in place of src[begin:end] needs parentheses to stay one operand: it does
// unless it is all of an argument, an interpolation, a value given to a name, a return or an expression statement.
func needsParens(src string, begin, end int) bool {
	before := strings.TrimRight(src[:begin], " \t")
	after := strings.TrimLeft(src[end:], " \t")
	open := before == "" || strings.ContainsAny(before[len(before)-1:], "=(,{:\n") || strings.HasSuffix(before, "return")
	closed := after == "" || strings.ContainsAny(after[:1], "),}\r\n")
	return !open || !closed
}

func templateParts(template string, args map[string][2]int) ([]templatePart, bool) {
	var parts []templatePart
	for template != "" {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			parts = append(parts, templatePart{literal: template})
			break
		}
		closing := strings.IndexByte(template[open:], '}')
		if closing < 0 {
			return nil, false
		}
		if open > 0 {
			parts = append(parts, templatePart{literal: template[:open]})
		}
		span, ok := args[template[open+1:open+closing]]
		if !ok {
			return nil, false
		}
		parts = append(parts, templatePart{begin: span[0], end: span[1], isArg: true})
		template = template[open+closing+1:]
	}
	return parts, true
}

// chainLeaf is the expression a chain of member steps, calls and subscripts starts from, and how many member steps
// lie between it and e's end.
func chainLeaf(e ast.Expr) (ast.Expr, int) {
	steps := 0
	for {
		if m, ok := e.AsMemberExpr(); ok {
			steps++
			e = m.Obj()
		} else if c, ok := e.AsCallExpr(); ok {
			e = c.Callee()
		} else if ix, ok := e.AsIndexExpr(); ok {
			e = ix.Obj()
		} else {
			return e, steps
		}
	}
}

// chainNames are the offsets of the names after each '.' of the postfix chain that starts at start.
func chainNames(src string, start int) []int {
	at := skipPrimary(src, start)
	var names []int
	for at < len(src) {
		switch {
		case src[at] == '.' && at+1 < len(src) && isNameByte(src[at+1]):
			names = append(names, at+1)
			at = skipName(src, at+1)
		case src[at] == '(' || src[at] == '[' || src[at] == '{':
			at = skipGroup(src, at)
		default:
			return names
		}
	}
	return names
}

// isPostfix is whether a template is one postfix chain, a name with its steps and calls, which stays one operand where
// it is written.
func isPostfix(t string) bool {
	at := skipPrimary(t, 0)
	for at < len(t) {
		switch {
		case t[at] == '.' && at+1 < len(t) && isNameByte(t[at+1]):
			at = skipName(t, at+1)
		case t[at] == '(' || t[at] == '[' || t[at] == '{':
			at = skipGroup(t, at)
		default:
			return false
		}
	}
	return true
}

func isNameByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// hasName is whether the name at at is name, and not only starts with it.
func hasName(src string, at int, name string) bool {
	return strings.HasPrefix(src[at:], name) && skipName(src, at) == at+len(name)
}

func skipName(src string, at int) int {
	for at < len(src) && isNameByte(src[at]) {
		at++
	}
	return at
}

func skipPrimary(src string, at int) int {
	switch {
	case at >= len(src):
		return at
	case src[at] == '"':
		return skipString(src, at)
	case src[at] == '(' || src[at] == '[' || src[at] == '{':
		return skipGroup(src, at)
	}
	return skipName(src, at)
}

// skipGroup is the offset after the bracket that closes the one at at, past strings and the groups inside.
func skipGroup(src string, at int) int {
	depth := 0
	for at < len(src) {
		switch src[at] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return at + 1
			}
		case '"':
			at = skipString(src, at)
			continue
		}
		at++
	}
	return at
}

// skipString is the offset after the string literal at at, whose interpolations are skipped as groups.
func skipString(src string, at int) int {
	at++
	for at < len(src) {
		switch src[at] {
		case '\\':
			at += 2
			continue
		case '"':
			return at + 1
		case '{':
			at = skipGroup(src, at)
			continue
		}
		at++
	}
	return at
}

// splitArgs are the spans of the comma-separated arguments in src[begin:end].
func splitArgs(src string, begin, end int) [][2]int {
	var out [][2]int
	if strings.TrimSpace(src[begin:end]) == "" {
		return nil
	}
	from := begin
	for at := begin; at < end; {
		switch src[at] {
		case '(', '[', '{':
			at = skipGroup(src, at)
			continue
		case '"':
			at = skipString(src, at)
			continue
		case ',':
			out = append(out, trimSpan(src, from, at))
			from = at + 1
		}
		at++
	}
	return append(out, trimSpan(src, from, end))
}

func trimSpan(src string, b, e int) [2]int {
	for b < e && (src[b] == ' ' || src[b] == '\t' || src[b] == '\n') {
		b++
	}
	for e > b && (src[e-1] == ' ' || src[e-1] == '\t' || src[e-1] == '\n') {
		e--
	}
	return [2]int{b, e}
}

// labelEnd is where the value of a labelled argument starts, after its 'label: '.
func labelEnd(src string, at int) int {
	at = skipName(src, at)
	if at < len(src) && src[at] == ':' {
		at++
	}
	for at < len(src) && src[at] == ' ' {
		at++
	}
	return at
}

// renderEdits writes text with the edits, outermost first; an edit inside a template's argument is written with it.
func renderEdits(text string, edits map[int]renameEdit) string {
	sorted := make([]renameEdit, 0, len(edits))
	for _, e := range edits {
		sorted = append(sorted, e)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].begin < sorted[j].begin })
	return renderSpan(text, 0, len(text), sorted)
}

func renderSpan(text string, begin, end int, edits []renameEdit) string {
	var out strings.Builder
	at := begin
	for i := range edits {
		e := edits[i]
		if e.begin < at || e.end > end {
			continue
		}
		out.WriteString(text[at:e.begin])
		inner := edits[i+1:]
		if e.parts == nil {
			out.WriteString(e.text)
		} else {
			for _, p := range e.parts {
				if p.isArg {
					out.WriteString(renderSpan(text, p.begin, p.end, within(inner, p.begin, p.end)))
				} else {
					out.WriteString(p.literal)
				}
			}
		}
		at = e.end
	}
	out.WriteString(text[at:end])
	return out.String()
}

func within(edits []renameEdit, begin, end int) []renameEdit {
	var out []renameEdit
	for _, e := range edits {
		if e.begin >= begin && e.end <= end {
			out = append(out, e)
		}
	}
	return out
}
