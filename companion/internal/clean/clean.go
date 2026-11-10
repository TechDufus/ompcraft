// Package clean implements the display-text cleaner of PROTOCOL §4.1.
package clean

import (
	"strings"
	"unicode"
)

// Clean drops private-use code points (U+E000–U+F8FF, U+F0000–U+10FFFF),
// everything outside the BMP, invalid UTF-8 and C0 controls other than
// whitespace, then collapses whitespace runs and trims. A run containing a
// newline collapses to one "\n", any other run to one space; leading and
// trailing whitespace is removed. Tab, CR, VT and FF count as whitespace, not
// as droppable controls.
func Clean(s string) string {
	s = strings.ToValidUTF8(s, "")
	var sb strings.Builder
	sb.Grow(len(s))
	pending := rune(0) // 0 none, ' ' or '\n'
	for _, r := range s {
		switch {
		case r == '\n' || r == 0x2028 || r == 0x2029 || r == 0x85:
			pending = '\n'
			continue
		case unicode.IsSpace(r):
			if pending == 0 {
				pending = ' '
			}
			continue
		case r < 0x20 || r == 0x7f:
			continue // other C0 controls (and DEL) are dropped
		case r > 0xFFFF, r >= 0xE000 && r <= 0xF8FF:
			continue
		}
		if pending != 0 && sb.Len() > 0 {
			sb.WriteRune(pending)
		}
		pending = 0
		sb.WriteRune(r)
	}
	return sb.String()
}

// OneLine is Clean with newlines replaced by single spaces.
func OneLine(s string) string {
	return strings.ReplaceAll(Clean(s), "\n", " ")
}
