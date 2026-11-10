package model

import (
	"strings"
	"unicode/utf8"

	"github.com/TechDufus/ompcraft/companion/internal/clean"
)

const (
	lastMaxBytes = 280
	footerSep    = "\n·\n"
	ellipsis     = "…"
)

// Clean implements PROTOCOL §4.1 `clean`; see package clean.
func Clean(s string) string { return clean.Clean(s) }

// oneLine is Clean with newlines replaced by single spaces.
func oneLine(s string) string { return clean.OneLine(s) }

// splitFooter cuts Clean-ed text at the first "\n·\n" (omp's turn-footer
// separator). A trailing "\n·" counts as a separator with an empty footer.
func splitFooter(c string) (body, footer string) {
	if i := strings.Index(c, footerSep); i >= 0 {
		return c[:i], c[i+len(footerSep):]
	}
	if strings.HasSuffix(c, "\n·") {
		return strings.TrimSuffix(c, "\n·"), ""
	}
	return c, ""
}

// Last implements PROTOCOL §4.1 `last`: Clean, cut at the first "\n·\n"
// (omp's turn-footer separator; a trailing "\n·" counts too), join lines with
// a space, truncate to 280 bytes on a UTF-8 boundary and append "…" if cut.
func Last(s string) string {
	c, _ := splitFooter(Clean(s))
	c = strings.TrimSpace(strings.ReplaceAll(c, "\n", " "))
	if len(c) <= lastMaxBytes {
		return c
	}
	cut := lastMaxBytes
	for cut > 0 && !utf8.RuneStart(c[cut]) {
		cut--
	}
	return strings.TrimRight(c[:cut], " ") + ellipsis
}

// Basename returns the last path element of cwd, accepting both separators,
// cleaned for display. "/" and "" give "".
func Basename(cwd string) string {
	cwd = strings.TrimRight(cwd, `/\`)
	if i := strings.LastIndexAny(cwd, `/\`); i >= 0 {
		cwd = cwd[i+1:]
	}
	return oneLine(cwd)
}

// Name implements PROTOCOL §4.1 `name`: Clean(title); when that is empty, "omp"
// or "zsh", the basename of cwd. If cwd gives nothing either, the cleaned title
// is kept, or "agent" when there is none.
func Name(title, cwd string) string {
	n := oneLine(title)
	if n == "" || n == "omp" || n == "zsh" {
		if b := Basename(cwd); b != "" {
			return b
		}
		if n == "" {
			return "agent"
		}
	}
	return n
}
