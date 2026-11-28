package main

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// An overlay is minimaxon's own version of declarations of one of maxon's library files, written in maxon's language under
// overlays/ at the file's path in maxon's stdlib directory, which the conversion puts in place of maxon's before it
// converts the file: a declaration of the overlay replaces maxon's of the same name, one maxon's file lacks is added at its
// end, and the names its first line lists after overlayDrops are taken out of maxon's file. It is for code that minimaxon
// compiles differently on purpose (README, "Where it differs from Maxon"), never for a rewrite the conversion can do. Every
// name must match, so an overlay that no longer fits the file fails the conversion.
//
//go:embed overlays
var overlays embed.FS

const (
	overlayRoot  = "overlays"
	overlayDrops = "// overlay drops:"
)

// applyOverlay is text, the source of the file at name, with the overlay for it applied, or text when it has none.
func applyOverlay(name, text string) (string, error) {
	overlay, found, err := overlayFor(name)
	if err != nil || !found {
		return text, err
	}
	return overlaid(name, text, overlay)
}

// overlaid is text with the declarations of overlay in place of its own.
func overlaid(name, text, overlay string) (string, error) {
	lines := strings.Split(strings.TrimSuffix(overlay, "\n"), "\n")
	var drops []string
	if rest, ok := strings.CutPrefix(lines[0], overlayDrops); ok {
		drops = strings.Fields(rest)
		lines = lines[1:]
	}
	units := segment(strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n"), "")
	for _, drop := range drops {
		at := unitNamed(units, drop)
		if at < 0 {
			return "", fmt.Errorf("%s: the overlay drops %q, which the file does not declare", name, drop)
		}
		units[at] = unit{}
	}
	for _, u := range segment(lines, "") {
		declared := unitName(u)
		if declared == "" {
			return "", fmt.Errorf("%s: the overlay has a comment that leads no declaration: %q", name, u.lines[0])
		}
		if at := unitNamed(units, declared); at >= 0 {
			u.gap = units[at].gap
			units[at] = u
			continue
		}
		units = append(units, u)
	}
	return joinUnits(units), nil
}

// overlayFor is the overlay whose path under overlays/ ends the path name, if there is one.
func overlayFor(name string) (string, bool, error) {
	slashed := "/" + filepath.ToSlash(name)
	var text string
	found := false
	err := fs.WalkDir(overlays, overlayRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return err
		}
		if strings.HasSuffix(slashed, "/"+strings.TrimPrefix(p, overlayRoot+"/")) {
			data, err := overlays.ReadFile(path.Clean(p))
			text, found = string(data), err == nil
			return err
		}
		return nil
	})
	return text, found, err
}

// unitNamed is the index of the unit that declares name, or -1.
func unitNamed(units []unit, name string) int {
	for i, u := range units {
		if unitName(u) == name {
			return i
		}
	}
	return -1
}

// unitName is the name a unit's declaration declares, or "" for a unit of comments alone.
func unitName(u unit) string {
	if u.header == "" {
		return ""
	}
	_, rest, _ := strings.Cut(declarationOf(u.header), " ")
	if end := strings.IndexAny(rest, " (=:"); end >= 0 {
		return rest[:end]
	}
	return rest
}
