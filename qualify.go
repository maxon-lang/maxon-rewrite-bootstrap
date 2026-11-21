package main

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/syntax"
	"os"
	"sort"
	"strings"
)

// qualification is a bare name of another module's declaration in a file, and the module path to write before it.
type qualification struct {
	offset    int
	qualifier string
}

// qualifyProjects is the qualify command: in each project, it writes the module path before every name that is written
// without one and that names a declaration of another module, which a program must qualify. A name is taken to mean
// what Maxon's bare lookup finds: the root module's declaration, else the one exported declaration of that name in the
// project. It reports the names it cannot place and leaves them alone, and leaves a project that does not parse alone.
func qualifyProjects(dirs []string) int {
	qualified, files, unplaced := 0, 0, 0
	for _, dir := range dirs {
		edits, missed, errs := projectQualifications(dir)
		if len(errs) > 0 {
			fmt.Printf("skipped  %s: %s\n", dir, errs[0])
			continue
		}
		unplaced += missed
		names := make([]string, 0, len(edits))
		for file := range edits {
			names = append(names, file)
		}
		sort.Strings(names)
		for _, file := range names {
			if err := applyQualifications(file, edits[file]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			qualified += len(edits[file])
			files++
		}
	}
	fmt.Printf("qualified %d names in %d files; %d names could not be placed\n", qualified, files, unplaced)
	return 0
}

// projectQualifications finds the qualifications of the project's files, by file, and how many names it could not place.
func projectQualifications(dir string) (map[string][]qualification, int, []*diag.CompileError) {
	var errs []*diag.CompileError
	proj := project.Discover(dir, &errs)
	if len(errs) > 0 {
		return nil, 0, errs
	}
	results := make([]ast.ModuleResult, len(proj.Modules))
	scratch := syntax.NewScratch()
	for i, m := range proj.Modules {
		diag.Guard(&errs, func() {
			src := scratch.LoadModuleSource(m.Files)
			program, syntaxErrors := scratch.Parse(src, scratch.Lex(src), m.ID)
			m.Adopt(program)
			errs = append(errs, syntaxErrors...)
		})
		if len(errs) == 0 {
			project.BuildModuleTable(m, &results[i].Errors)
		}
	}
	if len(errs) > 0 {
		proj.Locate(errs)
		return nil, 0, errs
	}
	edits := map[string][]qualification{}
	missed := 0
	for i, m := range proj.Modules {
		resolver := resolve.NewResolver(proj, m, &results[i])
		resolver.Unbound = func(name string, site ast.Pos) {
			qualifier, ok := owningModule(proj, m, name)
			if !ok {
				return
			}
			if qualifier == "" {
				file, line, col := proj.Where(site)
				fmt.Printf("unplaced %s:%d:%d: '%s' is declared by more than one module\n", file, line, col, name)
				missed++
				return
			}
			span := m.Positions.Files()[m.Positions.FileOf(int(site.Off))]
			edits[span.File] = append(edits[span.File], qualification{offset: int(site.Off) - span.Begin, qualifier: qualifier})
		}
		resolver.Run()
	}
	return edits, missed, nil
}

// owningModule is the qualifier of the declaration a bare name in module m means, or "" if more than one module could
// be meant; ok is false if no other module declares it.
func owningModule(proj *ast.Project, m *ast.Module, name string) (qualifier string, ok bool) {
	if _, declared := proj.Root.Names[name]; declared && m != proj.Root {
		return proj.Root.Qualifier(), true
	}
	var found []*ast.Module
	for _, other := range proj.Modules {
		if d, declared := other.Names[name]; declared && other != m && other != proj.Root && d.Base().IsExported() {
			found = append(found, other)
		}
	}
	switch len(found) {
	case 0:
		return "", false
	case 1:
		return found[0].Qualifier(), true
	}
	return "", true
}

// applyQualifications writes each qualifier and a dot before the name at its offset, once for each offset.
func applyQualifications(file string, quals []qualification) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", file, err)
	}
	sort.Slice(quals, func(i, j int) bool { return quals[i].offset < quals[j].offset })
	var out strings.Builder
	at := 0
	for i, q := range quals {
		if i > 0 && quals[i-1].offset == q.offset {
			continue
		}
		out.Write(data[at:q.offset])
		out.WriteString(q.qualifier + ".")
		at = q.offset
	}
	out.Write(data[at:])
	if err := os.WriteFile(file, []byte(out.String()), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", file, err)
	}
	return nil
}
