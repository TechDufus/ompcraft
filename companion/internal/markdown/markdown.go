// Package markdown turns an omp reply (markdown) into the line list of
// PROTOCOL §8.3: block kinds, depth for list items, and sentinel bytes for
// inline code and bold that the addon swaps for colour codes.
package markdown

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/TechDufus/ompcraft/companion/internal/clean"
)

// Line kinds.
const (
	KindHeading = "h"
	KindPara    = "p"
	KindItem    = "li"
	KindCode    = "code"
	KindQuote   = "quote"
	KindRule    = "hr"
	KindGap     = "gap"
	KindMore    = "more"
	KindTable   = "tr"
)

// Sentinel bytes in Line.T (never present in cleaned text).
const (
	CodeOpen  = "\x01"
	CodeClose = "\x02"
	BoldOpen  = "\x03"
	BoldClose = "\x04"
)

const (
	// MaxBytes caps the total of Line.T over all lines, the `more` line included.
	MaxBytes = 6000
	// MoreText is the text of the final `more` line when the reply was cut.
	MoreText = "… open in Tern for the full reply"
)

// Line is one rendered line. D is set (≥1) only for list items.
type Line struct {
	K string `lua:"k" json:"k"`
	T string `lua:"t" json:"t"`
	D int    `lua:"d,omitempty" json:"d,omitempty"`
}

var (
	fenceRe   = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})(.*)$")
	headingRe = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]+(.*?))?[ \t]*$`)
	closeHash = regexp.MustCompile(`[ \t]+#+[ \t]*$`)
	ruleRe    = regexp.MustCompile(`^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	quoteRe   = regexp.MustCompile(`^ {0,3}>[ \t]?(.*)$`)
	itemRe    = regexp.MustCompile(`^([ \t]*)(?:[-*+]|\d{1,9}[.)])(?:[ \t]+(.*))?$`)
	boldOnly  = regexp.MustCompile(`^\*\*(\S|\S.*?\S)\*\*$`)
	tableSep  = regexp.MustCompile(`^[|:\-\s]+$`)

	linkRe    = regexp.MustCompile(`!?\[([^\]]*)\]\((?:[^()\s]|\([^()]*\))*(?:\s+(?:"[^"]*"|'[^']*'))?\s*\)`)
	refLinkRe = regexp.MustCompile(`\[([^\]]+)\]\[[^\]]*\]`)
	autoRe    = regexp.MustCompile(`<(https?://[^>\s]+)>`)
	boldRe    = regexp.MustCompile(`\*\*(\S|\S.*?\S)\*\*`)
	strikeRe  = regexp.MustCompile(`~~(\S|\S.*?\S)~~`)
	starEmRe  = regexp.MustCompile(`(^|[^\w*])\*(\S|\S[^*]*?\S)\*($|[^\w*])`)
	underEmRe = regexp.MustCompile(`(^|[^\w])_(\S|\S[^_]*?\S)_($|[^\w])`)
	holderRe  = regexp.MustCompile("\x05(\\d+)\x06")
)

// Render converts markdown to lines (PROTOCOL §8.3).
func Render(src string) []Line {
	r := &renderer{}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	r.run(strings.Split(src, "\n"))
	return capLines(r.finish())
}

type pending struct {
	kind  string // "p", "li" or ""
	depth int
	parts []string
}

type renderer struct {
	out     []Line
	cur     pending
	gap     bool  // a blank line was seen since the last emitted line
	stack   []int // list indent stack
	size    int   // total bytes of T emitted so far
	inTable bool  // the previous line was a table row
}

func (r *renderer) emit(k, t string, d int) {
	if k != KindTable {
		r.inTable = false
	}
	if k != KindItem {
		r.stack = nil
	}
	if r.gap && len(r.out) > 0 && r.out[len(r.out)-1].K != KindGap {
		r.out = append(r.out, Line{K: KindGap})
	}
	r.gap = false
	r.size += len(t)
	r.out = append(r.out, Line{K: k, T: t, D: d})
}

// tableRow emits one table row as a `tr` line: cells trimmed, inline markup
// converted, joined with "  ·  ". The first row of a run (the header) gets its
// non-empty cells wrapped in bold sentinels. Rows with no text are skipped.
func (r *renderer) tableRow(row string) {
	header := !r.inTable
	cells := splitCells(row)
	out := make([]string, 0, len(cells))
	any := false
	for _, c := range cells {
		c = inline(clean.OneLine(c))
		if c != "" {
			any = true
			if header {
				c = BoldOpen + strings.NewReplacer(BoldOpen, "", BoldClose, "").Replace(c) + BoldClose
			}
		}
		out = append(out, c)
	}
	r.inTable = true
	if any {
		r.emit(KindTable, strings.Join(out, "  ·  "), 0)
	}
}

// splitCells splits a table row on unescaped pipes outside code spans after
// removing the outer pipes. `\|` becomes a literal pipe.
func splitCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	if strings.HasSuffix(row, "|") && !strings.HasSuffix(row, `\|`) {
		row = row[:len(row)-1]
	}
	var cells []string
	var sb strings.Builder
	inCode := false
	for i := 0; i < len(row); i++ {
		c := row[i]
		switch {
		case c == '\\' && i+1 < len(row) && row[i+1] == '|':
			sb.WriteByte('|')
			i++
		case c == '`':
			inCode = !inCode
			sb.WriteByte(c)
		case c == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(sb.String()))
			sb.Reset()
		default:
			sb.WriteByte(c)
		}
	}
	return append(cells, strings.TrimSpace(sb.String()))
}

func (r *renderer) flush() {
	c := r.cur
	r.cur = pending{}
	if c.kind == "" {
		return
	}
	text := clean.OneLine(strings.Join(c.parts, " "))
	if text == "" {
		return
	}
	r.emit(c.kind, inline(text), c.depth)
}

func indentOf(s string) int {
	n := 0
	for _, c := range s {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func (r *renderer) itemDepth(indent int) int {
	for len(r.stack) > 0 && indent < r.stack[len(r.stack)-1] {
		r.stack = r.stack[:len(r.stack)-1]
	}
	if len(r.stack) == 0 || indent >= r.stack[len(r.stack)-1]+2 {
		r.stack = append(r.stack, indent)
	}
	return len(r.stack)
}

func stripSentinels(s string) string {
	if !strings.ContainsAny(s, "\x01\x02\x03\x04") {
		return s
	}
	return strings.Map(func(c rune) rune {
		if c >= 1 && c <= 4 {
			return -1
		}
		return c
	}, s)
}

func (r *renderer) run(lines []string) {
	var (
		inFence  bool
		fenceCh  byte
		fenceLen int
		fenceInd int
	)
	for i := 0; i < len(lines) && r.size <= MaxBytes; i++ { // stop once the cap is certainly exceeded
		line := lines[i]
		if inFence {
			if m := fenceRe.FindStringSubmatch(line); m != nil && m[2][0] == fenceCh && len(m[2]) >= fenceLen && strings.TrimSpace(m[3]) == "" {
				inFence = false
				continue
			}
			// Strip up to the fence's own indent, keep the rest raw.
			k := 0
			for k < fenceInd && k < len(line) && line[k] == ' ' {
				k++
			}
			r.emit(KindCode, stripSentinels(strings.ToValidUTF8(line[k:], "")), 0)
			continue
		}
		if m := fenceRe.FindStringSubmatch(line); m != nil && !(m[2][0] == '`' && strings.Contains(m[3], "`")) {
			r.flush()
			inFence, fenceCh, fenceLen, fenceInd = true, m[2][0], len(m[2]), len(m[1])
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			r.flush()
			r.gap = true
			r.inTable = false
			continue
		case headingRe.MatchString(line):
			r.flush()
			m := headingRe.FindStringSubmatch(line)
			text := closeHash.ReplaceAllString(m[1], "")
			if text = clean.OneLine(text); text != "" {
				r.emit(KindHeading, inline(text), 0)
			}
			continue
		case ruleRe.MatchString(line):
			r.flush()
			r.emit(KindRule, "", 0)
			continue
		case quoteRe.MatchString(line):
			r.flush()
			text := line
			for {
				m := quoteRe.FindStringSubmatch(text)
				if m == nil {
					break
				}
				text = m[1]
			}
			if text = clean.OneLine(text); text == "" {
				r.gap = true
			} else {
				r.emit(KindQuote, inline(text), 0)
			}
			continue
		}
		if m := itemRe.FindStringSubmatch(line); m != nil {
			r.flush()
			if strings.TrimSpace(m[2]) == "" {
				continue // empty bullet
			}
			depth := r.itemDepth(indentOf(m[1]))
			r.cur = pending{kind: KindItem, depth: depth, parts: []string{m[2]}}
			continue
		}
		if strings.HasPrefix(trimmed, "|") { // a run of |-delimited rows is a table
			r.flush()
			if !tableSep.MatchString(trimmed) {
				r.tableRow(trimmed)
			}
			continue
		}
		if m := boldOnly.FindStringSubmatch(trimmed); m != nil && r.cur.kind == "" && !strings.Contains(m[1], "**") &&
			(i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == "") {
			text := clean.OneLine(m[1])
			r.emit(KindHeading, inline(text), 0)
			continue
		}
		switch {
		case r.cur.kind == KindItem && indentOf(line) >= 2:
			r.cur.parts = append(r.cur.parts, trimmed)
		case r.cur.kind == KindPara:
			r.cur.parts = append(r.cur.parts, trimmed)
		default:
			r.flush()
			r.cur = pending{kind: KindPara, parts: []string{trimmed}}
		}
	}
	r.flush()
}

func (r *renderer) finish() []Line {
	out := r.out
	for len(out) > 0 && out[len(out)-1].K == KindGap {
		out = out[:len(out)-1]
	}
	return out
}

// inline converts inline markup in already-cleaned text: code spans and bold
// become sentinels, links become their text, italics markers disappear.
func inline(s string) string {
	var codes []string
	var sb strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '`' {
			sb.WriteByte(s[i])
			i++
			continue
		}
		n := 1
		for i+n < len(s) && s[i+n] == '`' {
			n++
		}
		end := closingRun(s, i+n, n)
		if end < 0 {
			sb.WriteString(s[i : i+n])
			i += n
			continue
		}
		body := s[i+n : end]
		if len(body) > 1 && body[0] == ' ' && body[len(body)-1] == ' ' && strings.TrimSpace(body) != "" {
			body = body[1 : len(body)-1]
		}
		codes = append(codes, body)
		sb.WriteString("\x05" + strconv.Itoa(len(codes)-1) + "\x06")
		i = end + n
	}
	t := sb.String()
	t = linkRe.ReplaceAllString(t, "$1")
	t = refLinkRe.ReplaceAllString(t, "$1")
	t = autoRe.ReplaceAllString(t, "$1")
	t = boldRe.ReplaceAllString(t, BoldOpen+"$1"+BoldClose)
	t = strikeRe.ReplaceAllString(t, "$1")
	for range 2 { // a second pass catches matches that shared a boundary character
		t = starEmRe.ReplaceAllString(t, "$1$2$3")
		t = underEmRe.ReplaceAllString(t, "$1$2$3")
	}
	t = holderRe.ReplaceAllStringFunc(t, func(m string) string {
		idx, _ := strconv.Atoi(m[1 : len(m)-1])
		return CodeOpen + codes[idx] + CodeClose
	})
	return strings.TrimSpace(t)
}

// closingRun finds a run of exactly n backticks at or after from.
func closingRun(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			return i
		}
		i = j
	}
	return -1
}

// capLines enforces the 6000-byte total. When something is cut, the output
// ends with a `more` line; the content before it is cut on a UTF-8 boundary
// and any open sentinel is closed.
func capLines(lines []Line) []Line {
	budget := MaxBytes - len(MoreText)
	total := 0
	for _, l := range lines {
		total += len(l.T)
	}
	if total <= MaxBytes {
		return lines
	}
	used := 0
	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		if used+len(l.T) <= budget {
			out = append(out, l)
			used += len(l.T)
			continue
		}
		if room := budget - used; room > 0 && l.K != KindGap && l.K != KindRule {
			if t := cutBytes(l.T, room); t != "" {
				l.T = closeSentinels(t)
				out = append(out, l)
			}
		}
		break
	}
	for len(out) > 0 && out[len(out)-1].K == KindGap {
		out = out[:len(out)-1]
	}
	return append(out, Line{K: KindMore, T: MoreText})
}

// cutBytes cuts s to at most n bytes on a rune boundary, leaving room for
// the closers that closeSentinels may add.
func cutBytes(s string, n int) string {
	n -= 2
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func closeSentinels(s string) string {
	if strings.Count(s, CodeOpen) > strings.Count(s, CodeClose) {
		s += CodeClose
	}
	if strings.Count(s, BoldOpen) > strings.Count(s, BoldClose) {
		s += BoldClose
	}
	return s
}

// Preview returns one line of plain text from lines: headings, paragraphs,
// list items and quotes joined with spaces, sentinels removed, at most max
// bytes, cut on a UTF-8 boundary with "…" appended (the ellipsis counts
// toward max). Code lines are skipped.
func Preview(lines []Line, max int) string {
	var parts []string
	size := 0
	for _, l := range lines {
		switch l.K {
		case KindHeading, KindPara, KindItem, KindQuote:
		default:
			continue
		}
		t := strings.TrimSpace(stripSentinels(l.T))
		if t == "" {
			continue
		}
		parts = append(parts, t)
		size += len(t) + 1
		if size > max+8 {
			break
		}
	}
	s := strings.Join(parts, " ")
	if len(s) <= max {
		return s
	}
	const ell = "…"
	cut := max - len(ell)
	if cut < 0 {
		return ""
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ") + ell
}
