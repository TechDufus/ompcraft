package markdown

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	c1, c2 = "\x01", "\x02"
	b1, b2 = "\x03", "\x04"
)

func h(t string) Line         { return Line{K: KindHeading, T: t} }
func p(t string) Line         { return Line{K: KindPara, T: t} }
func li(d int, t string) Line { return Line{K: KindItem, T: t, D: d} }
func code(t string) Line      { return Line{K: KindCode, T: t} }
func quote(t string) Line     { return Line{K: KindQuote, T: t} }
func gap() Line               { return Line{K: KindGap} }
func hr() Line                { return Line{K: KindRule} }
func tr(t string) Line        { return Line{K: KindTable, T: t} }

const sep = "  ·  "

func TestRender(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Line
	}{
		{"empty", "", nil},
		{"only whitespace", " \n\t\n\r\n", nil},
		{"heading, paragraph, inline markup",
			"# Summary\n\nI fixed the **parser** bug in `lexer.go`.\nIt now handles [quotes](https://example.com/x) correctly.\n",
			[]Line{h("Summary"), gap(),
				p("I fixed the " + b1 + "parser" + b2 + " bug in " + c1 + "lexer.go" + c2 + ". It now handles quotes correctly.")}},
		{"heading levels and closing hashes",
			"## Title ##\n#hashtag\n####### seven\n",
			[]Line{h("Title"), p("#hashtag ####### seven")}},
		{"nested lists, ordered and unordered",
			"Steps:\n- first\n  - nested a\n  - nested b\n    - deep\n- second\n1. one\n2. two\n",
			[]Line{p("Steps:"), li(1, "first"), li(2, "nested a"), li(2, "nested b"), li(3, "deep"),
				li(1, "second"), li(1, "one"), li(1, "two")}},
		{"list item soft wrap and star bullets",
			"* alpha\n  continues here\n* beta\n\n  + gamma\n",
			[]Line{li(1, "alpha continues here"), li(1, "beta"), gap(), li(2, "gamma")}},
		{"loose list keeps one gap between items",
			"- a\n\n\n\n- b\n",
			[]Line{li(1, "a"), gap(), li(1, "b")}},
		{"fenced code with pipes, quotes, tabs and blank lines",
			"Run:\n\n```go\nfmt.Println(\"a | b\")\n\tif x { return }\n\n```\nDone.\n",
			[]Line{p("Run:"), gap(), code(`fmt.Println("a | b")`), code("\tif x { return }"), code(""), p("Done.")}},
		{"tilde fence, longer closing fence, markdown inside code stays raw",
			"~~~sh\n# not a heading\n**not bold** `x`\n~~~~\nafter\n",
			[]Line{code("# not a heading"), code("**not bold** `x`"), p("after")}},
		{"fence inside a list item is de-indented",
			"- step\n\n    ```\n    make test\n      indented\n    ```\n",
			[]Line{li(1, "step"), gap(), code("make test"), code("  indented")}},
		{"unterminated fence runs to the end",
			"```\nline one\nline two",
			[]Line{code("line one"), code("line two")}},
		{"inline code, bold, italics, strike",
			"Use `a ** b` and **bold with `code`** plus *italic* and _it_ and snake_case_name and 2 * 3 * 4 and ~~gone~~\n",
			[]Line{p("Use " + c1 + "a ** b" + c2 + " and " + b1 + "bold with " + c1 + "code" + c2 + b2 +
				" plus italic and it and snake_case_name and 2 * 3 * 4 and gone")}},
		{"double-backtick span may contain a backtick",
			"x ``a`b`` y `` padded `` z\n",
			[]Line{p("x " + c1 + "a`b" + c2 + " y " + c1 + "padded" + c2 + " z")}},
		{"links, images, autolinks, bare urls, reference links",
			"[x](http://a.b/c(d)) ![img](u \"t\") <https://e.f> bare https://g.h [ref][1] [plain]\n",
			[]Line{p("x img https://e.f bare https://g.h ref [plain]")}},
		{"quotes, hr and CRLF",
			"> note one\r\n> note two\r\n\r\n---\r\n\r\ntext\r\nwraps\r\n",
			[]Line{quote("note one"), quote("note two"), gap(), hr(), gap(), p("text wraps")}},
		{"nested quote markers and an empty quote line",
			">> deep\n>\n> back\n",
			[]Line{quote("deep"), gap(), quote("back")}},
		{"hr variants",
			"a\n\n***\n\n- - -\n\n___\n",
			[]Line{p("a"), gap(), hr(), gap(), hr(), gap(), hr()}},
		{"standalone bold line is a heading",
			"**Next steps**\n\n- a\n\n**Not alone** because text\nmore\n",
			[]Line{h("Next steps"), gap(), li(1, "a"), gap(), p(b1 + "Not alone" + b2 + " because text more")}},
		{"table: header bold, separator dropped, cells joined",
			"| a | b |\n|---|:--:|\n| 1 | 2 |\n",
			[]Line{tr(b1 + "a" + b2 + sep + b1 + "b" + b2), tr("1" + sep + "2")}},
		{"table from a real reply, empty header cell kept",
			"Anthropic's price list, per million tokens:\n\n| | Sonnet 5.5 | Opus 5.5 |\n|---|---|---|\n| Input | $2 | $4 |\n| Output | $10 | $20 |\n",
			[]Line{p("Anthropic's price list, per million tokens:"), gap(),
				tr(sep + b1 + "Sonnet 5.5" + b2 + sep + b1 + "Opus 5.5" + b2),
				tr("Input" + sep + "$2" + sep + "$4"),
				tr("Output" + sep + "$10" + sep + "$20")}},
		{"table cells: inline markup, code with a pipe, escaped pipe",
			"| Name | Use |\n|:--|--:|\n| **bold** | `a|b` and [x](http://y.z) |\n| a \\| b | _it_ |\n",
			[]Line{tr(b1 + "Name" + b2 + sep + b1 + "Use" + b2),
				tr(b1 + "bold" + b2 + sep + c1 + "a|b" + c2 + " and x"),
				tr("a | b" + sep + "it")}},
		{"two tables separated by a blank line each get a header; table right after a paragraph",
			"intro\n| a |\n|---|\n| 1 |\n\n| c |\n| 2 |\n",
			[]Line{p("intro"), tr(b1 + "a" + b2), tr("1"), gap(), tr(b1 + "c" + b2), tr("2")}},
		{"a table ends at the next block",
			"| a |\n|---|\n| 1 |\n- item\n| x |\n",
			[]Line{tr(b1 + "a" + b2), tr("1"), li(1, "item"), tr(b1 + "x" + b2)}},
		{"all-empty rows look like separators and are dropped; tables in code fences stay code",
			"| | |\n|--|--|\n| a | b |\n\n```\n| x | y |\n```\n",
			[]Line{tr(b1 + "a" + b2 + sep + b1 + "b" + b2), gap(), code("| x | y |")}},
		{"glyphs and astral characters are cleaned, except in code",
			"Done \U0001F680 ok \ue000\n\n```\nx \U0001F680 y\n```\n",
			[]Line{p("Done ok"), gap(), code("x \U0001F680 y")}},
		{"sentinel bytes in the source never survive",
			"a\x01b\x03c\n\n```\nx\x02y\x04\n```\n",
			[]Line{p("abc"), gap(), code("xy")}},
		{"never two gaps, none at the ends",
			"\n\n\nfirst\n\n\n\nsecond\n\n\n",
			[]Line{p("first"), gap(), p("second")}},
		{"real omp reply",
			"## Root cause\n\nThe `retry` loop in **`client.go`** never reset its backoff, so after one outage every call waited 30 s.\n\n" +
				"### Changes\n\n- `client.go`: reset `backoff` on success\n- `client_test.go`: add a regression test\n  - covers the 429 path\n  - covers *timeouts*\n\n" +
				"```go\nif err == nil {\n\tbackoff = 0\n}\n```\n\n> Note: run `go test ./...` before merging.\n\n**Verified**\n\nAll 14 tests pass.\n",
			[]Line{h("Root cause"), gap(),
				p("The " + c1 + "retry" + c2 + " loop in " + b1 + c1 + "client.go" + c2 + b2 + " never reset its backoff, so after one outage every call waited 30 s."), gap(),
				h("Changes"), gap(),
				li(1, c1+"client.go"+c2+": reset "+c1+"backoff"+c2+" on success"),
				li(1, c1+"client_test.go"+c2+": add a regression test"),
				li(2, "covers the 429 path"), li(2, "covers timeouts"), gap(),
				code("if err == nil {"), code("\tbackoff = 0"), code("}"), gap(),
				quote("Note: run " + c1 + "go test ./..." + c2 + " before merging."), gap(),
				h("Verified"), gap(), p("All 14 tests pass.")}},
	}
	for _, c := range cases {
		got := Render(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %#v\n want %#v", c.name, got, c.want)
		}
	}
}

func totalBytes(lines []Line) int {
	n := 0
	for _, l := range lines {
		n += len(l.T)
	}
	return n
}

func checkCapped(t *testing.T, name string, lines []Line) {
	t.Helper()
	if n := totalBytes(lines); n > MaxBytes {
		t.Errorf("%s: %d bytes of text, cap is %d", name, n, MaxBytes)
	}
	if len(lines) == 0 || lines[len(lines)-1] != (Line{K: KindMore, T: MoreText}) {
		t.Errorf("%s: last line is not the `more` line: %+v", name, lines[len(lines)-1])
	}
	for i, l := range lines {
		if !utf8.ValidString(l.T) {
			t.Errorf("%s: line %d is not valid UTF-8", name, i)
		}
		if l.K == KindMore && i != len(lines)-1 {
			t.Errorf("%s: `more` before the end", name)
		}
		if strings.Count(l.T, b1) != strings.Count(l.T, b2) || strings.Count(l.T, c1) != strings.Count(l.T, c2) {
			t.Errorf("%s: line %d has unbalanced sentinels: %q", name, i, l.T)
		}
	}
	if lines[len(lines)-2].K == KindGap {
		t.Errorf("%s: gap right before `more`", name)
	}
}

func TestRenderCap(t *testing.T) {
	// Many paragraphs of ASCII.
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("paragraph number with some words in it to fill space\n\n")
	}
	checkCapped(t, "ascii paragraphs", Render(sb.String()))

	// Multi-byte text in one very long paragraph: cut on a rune boundary.
	checkCapped(t, "multibyte", Render(strings.Repeat("日本語のテキスト ", 2000)))

	// A bold span cut in the middle is closed again.
	checkCapped(t, "bold span", Render("**"+strings.Repeat("a", 7000)+"**"))

	// An inline code span cut in the middle is closed again.
	checkCapped(t, "code span", Render("see `"+strings.Repeat("b", 7000)+"` now"))

	// A long fenced block.
	checkCapped(t, "code block", Render("```\n"+strings.Repeat("line of code with | pipes and \"quotes\"\n", 1000)+"```\n"))

	// A very long input (5 MB) still renders to the cap.
	big := strings.Repeat("- item with **bold** and `code`\n", 170000)
	if len(big) < 5_000_000 {
		t.Fatalf("fixture too small: %d", len(big))
	}
	checkCapped(t, "5 MB list", Render(big))

	// Exactly at the cap: nothing is cut.
	fit := strings.Repeat("x", MaxBytes)
	if got := Render(fit); len(got) != 1 || got[0].K != KindPara || len(got[0].T) != MaxBytes {
		t.Errorf("input that exactly fits was altered: %d lines", len(got))
	}
	if got := Render(fit + "y"); got[len(got)-1].K != KindMore {
		t.Error("input one byte over the cap must end with a `more` line")
	}
}

func TestPreview(t *testing.T) {
	lines := Render("## Result\n\nFixed the **parser** and `lexer`.\n\n```\nnoise\n```\n\n- one\n- two\n")
	if got, want := Preview(lines, 140), "Result Fixed the parser and lexer. one two"; got != want {
		t.Errorf("preview = %q, want %q", got, want)
	}
	if got := Preview(Render("```\nonly code\n```"), 140); got != "" {
		t.Errorf("code-only reply preview = %q, want empty", got)
	}
	long := Preview(Render(strings.Repeat("word ", 100)), 140)
	if len(long) > 140 || !strings.HasSuffix(long, "…") {
		t.Errorf("long preview: %d bytes %q", len(long), long)
	}
	multi := Preview(Render(strings.Repeat("日本語", 100)), 140)
	if len(multi) > 140 || !utf8.ValidString(multi) || !strings.HasSuffix(multi, "…") {
		t.Errorf("multibyte preview: %d bytes valid=%v", len(multi), utf8.ValidString(multi))
	}
	if got := Preview(Render("short"), 140); got != "short" {
		t.Errorf("short preview = %q", got)
	}
	for _, s := range []string{"\x01", "\x02", "\x03", "\x04"} {
		if strings.Contains(Preview(Render("a **b** `c`"), 140), s) {
			t.Errorf("sentinel %q leaked into the preview", s)
		}
	}
}
