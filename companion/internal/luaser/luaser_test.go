package luaser

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/TechDufus/ompcraft/companion/internal/luatest"
)

func TestStringEscaping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `"plain"`},
		{`a"b\c`, `"a\"b\\c"`},
		{"a\nb", `"a\nb"`},
		{"a|b||c", `"a||b||||c"`},
		{"|cff0000ffred|r", `"||cff0000ffred||r"`},
		{"tab\tcr\r", `"tab\009cr\013"`},
		{"\x00\x01\x1f\x7f", `"\000\001\031\127"`},
		{"é", `"\195\169"`},
		{"\x01" + "2", `"\0012"`},
		{"", `""`},
	}
	for _, c := range cases {
		if got := String(c.in); got != c.want {
			t.Errorf("String(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func allBytes() string {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return string(b)
}

func TestRoundTripThroughLua(t *testing.T) {
	luatest.Available(t)
	tree := map[string]any{
		"v":      0,
		"gen":    int64(17),
		"run":    int64(1762727000123),
		"big":    int64(math.MaxInt32) * 1000,
		"neg":    -42,
		"pi":     3.14159,
		"tenth":  0.1,
		"huge":   1e21,
		"tern":   true,
		"off":    false,
		"empty":  []any{},
		"emptyM": map[string]any{},
		"strings": []any{
			"plain",
			`with "quotes" and \ backslash`,
			"pipe | here || and |cff00ff00color|r",
			"line1\nline2\n\nline4",
			"tab\tcr\r\x0b\x0c",
			"multi-byte é 日本語 🙂 \U000F0000",
			"control \x00\x01\x1f\x7f bytes",
			"invalid utf8 \xff\xfe\xc3",
			"\x01" + "23 digits after escape",
			allBytes(),
			"",
		},
		"nested": map[string]any{
			"a": []any{
				map[string]any{"k": "v", "n": 1},
				[]any{"x", []any{"y"}},
			},
		},
		// keys that are keywords, not identifiers, pipes, non-ASCII
		"end": 1, "goto": 2, "nil": 3, "1st": 4, "a b": 5, "x|y": 6, "é": 7, "_ok9": 8, "": 9,
		`quote"key`: 10, "new\nline": 11,
	}
	data, err := Marshal("OmpcraftInbox", tree)
	if err != nil {
		t.Fatal(err)
	}
	got := luatest.Run(t, data)
	want := luatest.Expect(tree)
	if !reflect.DeepEqual(got, want) {
		for i := range max(len(got), len(want)) {
			var g, w string
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Fatalf("first mismatch at %d:\n lua : %s\n want: %s", i, g, w)
			}
		}
	}
}

type inner struct {
	Name string `lua:"name"`
	Skip string `lua:"-"`
	Ptr  *int   `lua:"ptr"`
}

type outer struct {
	Items  []inner           `lua:"items"`
	Nil    []inner           `lua:"nilslice"`
	Map    map[string]string `lua:"map"`
	Flag   bool              `lua:"flag"`
	Count  uint32            `lua:"count"`
	F      float32           `lua:"f"`
	hidden int
}

func TestStructAndMap(t *testing.T) {
	luatest.Available(t)
	o := outer{
		Items: []inner{{Name: "a|b", Skip: "never"}, {Name: "c"}},
		Map:   map[string]string{"z": "1", "a": "2"},
		Flag:  true,
		Count: 7,
		F:     0.5,
	}
	data, err := Marshal("OmpcraftInbox", o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "never") || strings.Contains(string(data), "ptr") {
		t.Fatalf("skipped or nil fields leaked:\n%s", data)
	}
	// Deterministic: struct order, sorted map keys.
	data2, _ := Marshal("OmpcraftInbox", o)
	if string(data) != string(data2) {
		t.Fatal("output is not deterministic")
	}
	if strings.Index(string(data), "items") > strings.Index(string(data), "flag") {
		t.Fatalf("struct field order not kept:\n%s", data)
	}
	if strings.Index(string(data), "a = ") > strings.Index(string(data), "z = ") {
		t.Fatalf("map keys not sorted:\n%s", data)
	}
	want := luatest.Expect(map[string]any{
		"items":    []any{map[string]any{"name": "a|b"}, map[string]any{"name": "c"}},
		"nilslice": []any{},
		"map":      map[string]any{"z": "1", "a": "2"},
		"flag":     true,
		"count":    7,
		"f":        0.5,
	})
	if got := luatest.Run(t, data); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestNumberFormats(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int64(math.MaxInt64), "9223372036854775807"},
		{int64(math.MinInt64), "-9223372036854775808"},
		{uint64(5), "5"},
		{3.0, "3"},
		{0.1, "0.1"},
		{1e21, "1e+21"},
		{-2.5, "-2.5"},
	}
	for _, c := range cases {
		b, err := Marshal("X", c.in)
		if err != nil {
			t.Fatal(err)
		}
		if want := "X = " + c.want + "\n"; string(b) != want {
			t.Errorf("Marshal(%v) = %q, want %q", c.in, b, want)
		}
	}
}

func TestRejects(t *testing.T) {
	type noTag struct{ A int }
	deep := any("x")
	for range maxDepth + 2 {
		deep = []any{deep}
	}
	cases := map[string]any{
		"NaN":         math.NaN(),
		"+Inf":        math.Inf(1),
		"-Inf":        math.Inf(-1),
		"uint64 max":  uint64(math.MaxUint64),
		"missing tag": noTag{1},
		"int keys":    map[int]string{1: "a"},
		"chan":        make(chan int),
		"too deep":    deep,
	}
	for name, v := range cases {
		if _, err := Marshal("X", v); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := Marshal("end", 1); err == nil {
		t.Error("keyword global name accepted")
	}
	if _, err := Marshal("a b", 1); err == nil {
		t.Error("invalid global name accepted")
	}
}

func TestOutputIsLiteralOnly(t *testing.T) {
	// Hostile text must stay inside string literals: nothing but data between quotes.
	evil := `"}; os.execute("x") --` + "\n" + `]]; OmpcraftInbox = nil --`
	b, err := Marshal("OmpcraftInbox", map[string]any{"s": evil})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "\n") != 3 { // open brace, one field, close brace
		t.Fatalf("unexpected layout:\n%s", b)
	}
	got := luatest.Run(t, b)
	want := luatest.Expect(map[string]any{"s": evil})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

type emptyOpt struct {
	S  string   `lua:"s,omitempty"`
	N  int      `lua:"n,omitempty"`
	B  bool     `lua:"b,omitempty"`
	L  []string `lua:"l,omitempty"`
	P  *inner   `lua:"p,omitempty"`
	K  string   `lua:"keep"`
	Z  int      `lua:"zero"`
	S2 string   `lua:"s2,omitempty"`
}

func TestOmitEmpty(t *testing.T) {
	b, err := Marshal("X", emptyOpt{S2: "set"})
	if err != nil {
		t.Fatal(err)
	}
	want := "X = {\n  keep = \"\",\n  zero = 0,\n  s2 = \"set\",\n}\n"
	if string(b) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b, want)
	}
	b, err = Marshal("X", emptyOpt{S: "a", N: 2, B: true, L: []string{"x"}, P: &inner{Name: "n"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{`s = "a"`, "n = 2", "b = true", "l = {", "p = {", `keep = ""`} {
		if !strings.Contains(string(b), f) {
			t.Errorf("missing %s in:\n%s", f, b)
		}
	}
}
