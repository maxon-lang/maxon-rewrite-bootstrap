package main

import (
	"slices"
	"strings"
)

// Spec files keep maxon's format (docs/SPECS.md in the maxon repository): a test is a `<!-- test: name -->` line and
// the fenced blocks after it, up to the next test or `## ` heading, and only a ```maxon block is source. Everything
// else in the file is copied as it is.
const (
	specExt           = ".md"
	specTestMarker    = "<!-- test:"
	specCommentClose  = "-->"
	specTestsHeading  = "## Tests"
	specHeadingPrefix = "## "
	specFenceMark     = "```"
	specSourceInfo    = "maxon"

	// A test that converted with warnings carries one `<!-- skip: kind -->` line after its test marker for each
	// distinct warning message in the test, sorted. The runner skips and counts such a test. A line each, because a
	// message holds any punctuation a separator could be.
	specSkipMarker = "<!-- skip: "

	// A test that expects a compiler error (its expectation names positions of the source as maxon laid it out) and
	// whose converted block is laid out differently carries this line after its test marker, so that the runner compares
	// the codes and messages of the errors and not their positions.
	specPositionsMarker = "<!-- positions: moved -->"

	// A ```maxon block of several files is split by these lines; each section is its own source file, and a runtime
	// file or a stdlib overlay is source too.
	specFileMarker    = "// --- file:"
	specOverlayMarker = "// --- stdlib-overlay:"
	specRuntimeMarker = "// --- runtime-file:"

	// The kind of a test whose converted source minimaxon does not parse with no construct to blame (a warning explains
	// the rest): a syntax maxon accepts and minimaxon lacks, or a layout the tool does not fix yet. It is followed by
	// the parser's message.
	rejectedKindPrefix = "minimaxon does not parse the converted source: "

	// A test that expects an error of maxon's lexer or parser (an E1xxx or E2xxx code) is about the source not parsing,
	// so the source stays as converted and the runner compares the error.
	lexerErrorPrefix  = "error E1"
	syntaxErrorPrefix = "error E2"
	errorExpectation  = "maxoncstderr"
)

var specSectionMarkers = []string{specFileMarker, specOverlayMarker, specRuntimeMarker}

// maxon's one `acquire` block takes the states of several services at once (its reference, "Atomic Updates"), which
// minimaxon drops (README, Where it differs from Maxon: "There is no `acquire`"). A test of one that expects a code of
// maxon's region rules on the states the block holds pins a rule that minimaxon's owned values, borrows and frozen values
// replace, so it is skipped for that; both are permanent differences.
const (
	serviceAcquireWarning = "language difference: maxon's acquire block, which takes the states of several services at once, is dropped: states that share an invariant are one service (README, Where it differs from Maxon: there is no acquire)"
	regionAcquireWarning  = "language difference: the test pins a rule of maxon's regions on the states an acquire block holds, and the regions are replaced by owned values, borrows and frozen values, so the rule does not exist (README, Where it differs from Maxon: maxon's codes for the rules of its regions are not raised)"
)

// regionCodes are maxon's codes for the rules of its regions, which minimaxon does not raise: the codes of the region
// `expect` row of self/driver/skips.tsv.
var regionCodes = []string{"E3196", "E3198", "E3199", "E3200", "E3201", "E3202", "E3203", "E3205", "E3206", "E3207", "E3208", "E3209", "E3211", "E3214", "E3226"}

// SpecTest is one test of a spec file and the kinds of warning that block it.
type SpecTest struct {
	Name  string
	Skips []string
}

// SpecResult is a converted spec file. Tests is empty for a file that is not a spec.
type SpecResult struct {
	Text     string
	Tests    []SpecTest
	Warnings []Warning
	// Changed reports whether Text differs from the file's text with line endings normalized.
	Changed bool
}

// ConvertSpec converts the ```maxon blocks of every test in a spec file and marks each test that warned. Line endings
// become LF; a block's layout follows Convert.
func ConvertSpec(file, src string) SpecResult {
	text := strings.ReplaceAll(strings.TrimPrefix(src, utf8BOM), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	c := specConverter{file: file}
	inTests := false

	for i := 0; i < len(lines); {
		line := lines[i]
		if fence, ok := openingFence(line); ok {
			end := closingFence(lines, i, fence)
			if inTests && c.open && strings.TrimSpace(strings.TrimPrefix(line, fence)) == specSourceInfo {
				c.convertBlock(lines[i+1:end], i+2)
				c.out = append(c.out, line)
				c.out = append(c.out, c.block...)
				if end < len(lines) {
					c.out = append(c.out, lines[end])
				}
			} else {
				if c.open && isErrorExpectation(line) {
					if !c.expectsError {
						c.firstExpected = firstErrorCode(lines[i+1 : end])
					}
					c.expectsError = true
					c.expectsSyntaxError = c.expectsSyntaxError || hasSyntaxError(lines[i+1:end])
				}
				if c.open && inTests && expectationNamesMailbox(lines[i+1:end]) {
					c.kinds = append(c.kinds, mailboxWarning)
				}
				c.out = append(c.out, lines[i:min(end+1, len(lines))]...)
			}
			i = end + 1
			continue
		}
		switch {
		case c.open && strings.HasPrefix(line, specSkipMarker):
			// Regenerated from this conversion's own warnings.
		case c.open && line == specPositionsMarker:
			// The original's layout is gone from converted text, so the mark it carries stays.
			c.moved = true
		case strings.HasPrefix(line, specTestMarker):
			c.closeTest()
			c.startTest(line)
			c.names = programNamesOf(testSource(lines, i+1))
		case strings.HasPrefix(line, specHeadingPrefix):
			c.closeTest()
			if strings.TrimRight(line, " \t") == specTestsHeading {
				inTests = true
			}
			c.out = append(c.out, line)
		default:
			c.out = append(c.out, line)
		}
		i++
	}
	c.closeTest()

	if len(c.tests) == 0 {
		return SpecResult{Text: text}
	}
	out := strings.Join(c.out, "\n")
	return SpecResult{Text: out, Tests: c.tests, Warnings: c.warnings, Changed: out != text}
}

// specConverter holds the state of one spec file's conversion.
type specConverter struct {
	file     string
	out      []string
	tests    []SpecTest
	warnings []Warning

	// open is set between a test marker and the end of its case; markerAt is where the marker is in out, so the skip
	// line can follow it once the whole case has been seen.
	open     bool
	markerAt int
	kinds    []string

	// moved is whether a block of the case is laid out differently from the original, so that a position in the
	// expectation no longer names the same text; expectsError is whether the case has a compiler-error expectation.
	moved        bool
	expectsError bool
	// firstExpected is the code of the first error the case's first compiler-error expectation names, or empty.
	firstExpected string

	// rejected holds the kinds of the case's blocks that minimaxon does not parse; expectsSyntaxError is whether the
	// case expects such an error, which makes them the point of the case and no reason to skip it.
	rejected           []string
	expectsSyntaxError bool

	// block is the converted lines of the block convertBlock handled last.
	block []string

	// names is what the open test's program, all its files together, says about the names it writes.
	names programNames
}

func (c *specConverter) startTest(marker string) {
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(marker, specTestMarker)), specCommentClose))
	c.tests = append(c.tests, SpecTest{Name: name})
	c.open = true
	c.markerAt = len(c.out)
	c.kinds = nil
	c.rejected = nil
	c.moved = false
	c.expectsError = false
	c.firstExpected = ""
	c.expectsSyntaxError = false
	c.out = append(c.out, marker)
}

func (c *specConverter) closeTest() {
	if !c.open {
		return
	}
	c.open = false
	if !c.expectsSyntaxError {
		c.kinds = append(c.kinds, c.rejected...)
	}
	if slices.Contains(regionCodes, c.firstExpected) {
		for i, kind := range c.kinds {
			if kind == serviceAcquireWarning {
				c.kinds[i] = regionAcquireWarning
			}
		}
	}
	slices.Sort(c.kinds)
	c.kinds = slices.Compact(c.kinds)
	c.tests[len(c.tests)-1].Skips = c.kinds
	var marks []string
	for _, kind := range c.kinds {
		marks = append(marks, specSkipMarker+kind+" "+specCommentClose)
	}
	if c.moved && c.expectsError {
		marks = append(marks, specPositionsMarker)
	}
	c.out = slices.Insert(c.out, c.markerAt+1, marks...)
}

// convertBlock converts the lines of one ```maxon block into c.block. firstLine is the 1-based line of the file that
// the block's first line is on.
func (c *specConverter) convertBlock(body []string, firstLine int) {
	c.block = c.block[:0]
	start := 0
	section := func(end int) {
		c.block = append(c.block, c.convertSection(body[start:end], firstLine+start)...)
	}
	for i, line := range body {
		if isSectionMarker(line) {
			section(i)
			c.block = append(c.block, line)
			start = i + 1
		}
	}
	section(len(body))
	if !sameLayout(body, c.block) {
		c.moved = true
	}
}

// sameLayout reports whether the converted lines are the original's, line for line, so that a position of the original
// names the same text in the converted block. Trailing whitespace carries no position.
func sameLayout(original, converted []string) bool {
	return slices.EqualFunc(original, converted, func(a, b string) bool {
		return strings.TrimRight(a, " 	") == strings.TrimRight(b, " 	")
	})
}

// convertSection converts one source file's lines, keeping the blank lines around it: maxon trims a section, so they
// carry no meaning, but they keep the sections apart as the author laid them out.
func (c *specConverter) convertSection(body []string, firstLine int) []string {
	first, last := 0, len(body)
	for first < last && strings.TrimSpace(body[first]) == "" {
		first++
	}
	for last > first && strings.TrimSpace(body[last-1]) == "" {
		last--
	}
	if first == last {
		return body
	}
	res := convertProgram(c.file, strings.Join(body[first:last], "\n"), specProgram, c.names)
	res.Text = moveLibraryListing(res.Text)
	for _, w := range res.Warnings {
		w.Line += firstLine + first - 1
		c.warnings = append(c.warnings, w)
		c.kinds = append(c.kinds, w.Message)
	}
	if !res.Formatted && len(res.Warnings) == 0 {
		c.rejected = append(c.rejected, rejectedKindPrefix+res.Rejected)
	}
	out := append([]string{}, body[:first]...)
	out = append(out, strings.Split(strings.TrimSuffix(res.Text, "\n"), "\n")...)
	return append(out, body[last:]...)
}

const (
	// maxonLibraryListing is how a spec lists maxon's stdlib directory, which holds maxon's files, and libraryFileSought the
	// file of it that such a spec looks for.
	maxonLibraryListing = `Directory.list(FilePath from "../stdlib")`
	libraryFileSought   = `"String.maxon"`
	// minimaxonLibraryListing is the directory that holds that file here: the library's root holds no file, only modules
	// (README, "Modules"), and `String.maxon` is in the module of strings.
	minimaxonLibraryListing = `Directory.list(FilePath from "../stdlib/string")`
)

// moveLibraryListing makes a spec that lists maxon's stdlib directory for `String.maxon` list the directory that holds the
// file in minimaxon's library.
func moveLibraryListing(text string) string {
	if !strings.Contains(text, libraryFileSought) {
		return text
	}
	return strings.ReplaceAll(text, maxonLibraryListing, minimaxonLibraryListing)
}

// testSource is the source of every ```maxon block of the test whose lines start at from, up to the next test or heading.
func testSource(lines []string, from int) string {
	var src []string
	for i := from; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, specTestMarker) || strings.HasPrefix(line, specHeadingPrefix) {
			break
		}
		if fence, ok := openingFence(line); ok {
			end := closingFence(lines, i, fence)
			if strings.TrimSpace(strings.TrimPrefix(line, fence)) == specSourceInfo {
				src = append(src, lines[i+1:min(end, len(lines))]...)
			}
			i = end
		}
	}
	return strings.Join(src, "\n")
}

func isSectionMarker(line string) bool {
	for _, marker := range specSectionMarkers {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	return false
}

// openingFence returns the run of backticks that opens a fenced block on the line.
func openingFence(line string) (string, bool) {
	n := len(line) - len(strings.TrimLeft(line, "`"))
	if n < len(specFenceMark) {
		return "", false
	}
	return line[:n], true
}

// closingFence returns the index of the line that closes the block opened at lines[open], or len(lines) when the file
// ends inside it. A block closes on a line of nothing but backticks, at least as many as opened it.
func closingFence(lines []string, open int, fence string) int {
	for i := open + 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		if strings.Trim(line, "`") == "" && len(line) >= len(fence) {
			return i
		}
	}
	return len(lines)
}

// isErrorExpectation reports whether the fence line opens a compiler-error block, with or without a target qualifier.
func isErrorExpectation(fenceLine string) bool {
	info := strings.TrimLeft(fenceLine, "`")
	return strings.HasPrefix(strings.ToLower(info), errorExpectation)
}

// firstErrorCode is the code of the first error line of an expectation block, or empty.
func firstErrorCode(block []string) string {
	for _, line := range block {
		if rest, ok := strings.CutPrefix(line, "error E"); ok && len(rest) >= 4 {
			return "E" + rest[:4]
		}
	}
	return ""
}

func hasSyntaxError(block []string) bool {
	return slices.ContainsFunc(block, func(line string) bool {
		return strings.HasPrefix(line, lexerErrorPrefix) || strings.HasPrefix(line, syntaxErrorPrefix)
	})
}
