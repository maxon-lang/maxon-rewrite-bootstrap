package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// generalCategoryFile is the file the General_Category table is made from: Unicode 16.0.0.
const generalCategoryFile = "ucd/DerivedGeneralCategory.txt"

// categoryOutput is the library's file of the General_Category table, which maxon's CharacterSet reads through
// unicodeGeneralCategory: in the characterset module, which stands in for maxon's stdlib/helpers/string/unicodeCategory.maxon
// that reads the table from binary files that minimaxon does not read.
const categoryOutput = "../../stdlib/characterset/unicodeCategory_gen.maxon"

const (
	// A category entry is the first code point of a run of one category, shifted up by categoryBits, and the category, in
	// tableDigits digits of tableDigitBits bits.
	categoryBits = 5
	// unassignedCategory is Cn, the category of every code point the data file does not name.
	unassignedCategory = 0
)

// categories are maxon's numbers of the General_Category values (maxon's scripts/unicode/generate_unicode_category.py):
// Cn is 0, and the others follow in the order of their names within each major class.
var categories = []string{
	"Cn",
	"Lu", "Ll", "Lt", "Lm", "Lo",
	"Mn", "Mc", "Me",
	"Nd", "Nl", "No",
	"Pc", "Pd", "Pe", "Pf", "Pi", "Po", "Ps",
	"Sc", "Sk", "Sm", "So",
	"Zs", "Zl", "Zp",
	"Cc", "Cf", "Co", "Cs",
}

// categoryMasks are maxon's masks of categories that CharacterSet's predefined sets hold, with their names.
var categoryMasks = []struct {
	name    string
	members []string
}{
	{"GC_MASK_LETTERS", []string{"Lu", "Ll", "Lt", "Lm", "Lo", "Mn", "Mc", "Me"}},
	{"GC_MASK_LOWERCASE", []string{"Ll"}},
	{"GC_MASK_UPPERCASE", []string{"Lu", "Lt"}},
	{"GC_MASK_DIGITS", []string{"Nd"}},
	{"GC_MASK_ALPHANUMERICS", []string{"Lu", "Ll", "Lt", "Lm", "Lo", "Mn", "Mc", "Me", "Nd", "Nl", "No"}},
	{"GC_MASK_PUNCTUATION", []string{"Pc", "Pd", "Pe", "Pf", "Pi", "Po", "Ps"}},
	{"GC_MASK_SYMBOLS", []string{"Sc", "Sk", "Sm", "So"}},
	{"GC_MASK_CONTROL", []string{"Cc", "Cf"}},
	{"GC_MASK_WHITESPACE_ZS", []string{"Zs"}},
	{"GC_MASK_LINE_PARA_SEP", []string{"Zl", "Zp"}},
}

// categoryNumber is the number of a General_Category value.
func categoryNumber(name string) (int, bool) {
	for i, c := range categories {
		if c == name {
			return i, true
		}
	}
	return 0, false
}

// readCategories is the category of every code point, from the data file.
func readCategories() ([]byte, error) {
	data, err := os.ReadFile(generalCategoryFile)
	if err != nil {
		return nil, err
	}
	category := make([]byte, codePointCount)
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Split(line, ";")
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimSpace(fields[1])
		n, ok := categoryNumber(name)
		if !ok {
			return nil, fmt.Errorf("%s: unknown category %q", generalCategoryFile, name)
		}
		first, last, err := parseRange(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("%s: %v", generalCategoryFile, err)
		}
		for cp := first; cp <= last; cp++ {
			category[cp] = byte(n)
		}
	}
	return category, sc.Err()
}

// generateCategory is the text of the library's General_Category file.
func generateCategory() (string, error) {
	category, err := readCategories()
	if err != nil {
		return "", err
	}
	if codePointCount<<categoryBits >= 1<<(tableDigits*tableDigitBits) || len(categories) > 1<<categoryBits {
		return "", fmt.Errorf("a category entry does not fit %d digits", tableDigits)
	}
	var table strings.Builder
	entries := 0
	for cp := range codePointCount {
		if cp > 0 && category[cp] == category[cp-1] {
			continue
		}
		value := cp<<categoryBits | int(category[cp])
		for d := tableDigits - 1; d >= 0; d-- {
			table.WriteByte(digit(value >> (d * tableDigitBits) & (1<<tableDigitBits - 1)))
		}
		entries++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// %s\n\n", header)
	b.WriteString("// GeneralCategory is maxon's: the number of a code point's Unicode General_Category, Cn (unassigned) 0 and the others in\n" +
		"// the order of their names within each major class.\n")
	fmt.Fprintf(&b, "export typealias GeneralCategory = int(%d to %d)\n\n", unassignedCategory, len(categories)-1)
	b.WriteString("// The masks of the categories of CharacterSet's predefined sets: a bit for each category, at its number.\n")
	for _, m := range categoryMasks {
		mask := 0
		for _, name := range m.members {
			n, ok := categoryNumber(name)
			if !ok {
				return "", fmt.Errorf("mask %s names unknown category %q", m.name, name)
			}
			mask |= 1 << n
		}
		fmt.Fprintf(&b, "export let %s = %d\n\n", m.name, mask)
	}
	b.WriteString("// categoryTableDigits is the number of characters of an entry of the table: the first code point of a run of one category,\n" +
		"// shifted up by categoryTableCategoryBits, and the category, in categoryTableDigitBits bits a character, the first first.\n")
	fmt.Fprintf(&b, "let categoryTableDigits = %d as Count\n\n", tableDigits)
	fmt.Fprintf(&b, "let categoryTableDigitBits = %d as Count\n\n", tableDigitBits)
	fmt.Fprintf(&b, "let categoryTableCategoryBits = %d as Count\n\n", categoryBits)
	fmt.Fprintf(&b, "let categoryTableCategoryMask = %d as Count\n\n", 1<<categoryBits-1)
	b.WriteString("// categoryTableDigitFirst is the character of the digit 0; a character above categoryTableDigitEscape is the digit one more\n" +
		"// than its distance from it, since a string literal does not hold that backslash.\n")
	fmt.Fprintf(&b, "let categoryTableDigitFirst = %d as Count\n\n", digitFirst)
	fmt.Fprintf(&b, "let categoryTableDigitEscape = %d as Count\n\n", digitEscape)
	fmt.Fprintf(&b, "let categoryTableEntryCount = %d as Count\n\n", entries)
	b.WriteString(`// categoryTableDigit is the value of the character of the table at a position.
function categoryTableDigit(at Count) returns Count
	let character = categoryTable.byteAt(at) as Count
	if character > categoryTableDigitEscape 'afterEscape'
		return try character - categoryTableDigitFirst - 1 otherwise panic("a category table digit below its first")
	end 'afterEscape'
	return try character - categoryTableDigitFirst otherwise panic("a category table digit below its first")
end 'categoryTableDigit'

// categoryTableEntry is the number of the entry of the table at an index: the first code point of a run of one category,
// shifted up by categoryTableCategoryBits, and the category.
function categoryTableEntry(index Count) returns Count
	let first = try index * categoryTableDigits otherwise panic("a category table position overflows")
	var value = 0 as Count
	for i in 0 upto categoryTableDigits 'digits'
		value = try (value shl categoryTableDigitBits) + categoryTableDigit(try first + i otherwise panic("a category table position overflows")) otherwise panic("a category table entry overflows")
	end 'digits'
	return value
end 'categoryTableEntry'

// unicodeGeneralCategory is the General_Category of a code point: that of the last run of the table that starts at or before
// it, which the entry at 0 always is.
export function unicodeGeneralCategory(cp Codepoint) returns GeneralCategory
	let code = cp as Count
	var low = 0 as Count
	var high = categoryTableEntryCount
	while (try high - low otherwise panic("a category search crossed itself")) > 1 'search'
		let middle = try low + (high - low) / 2 otherwise panic("a category search crossed itself")
		if categoryTableEntry(middle) shr categoryTableCategoryBits <= code 'right'
			low = middle
		end 'right' else 'left'
			high = middle
		end 'left'
	end 'search'
	return try (categoryTableEntry(low) and categoryTableCategoryMask) as GeneralCategory otherwise panic("a category table entry names no category")
end 'unicodeGeneralCategory'

`)
	b.WriteString("// categoryTable is the table of the categories: one entry for each run of code points of one category.\n")
	fmt.Fprintf(&b, "let categoryTable = \"%s\"\n", table.String())
	return b.String(), nil
}
