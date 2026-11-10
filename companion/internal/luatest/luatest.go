// Package luatest runs generated data.lua files through a real lua5.1 and
// dumps the resulting table canonically, for round-trip tests.
package luatest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const driver = `
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local out = {}
local function dump(path, v)
  local t = type(v)
  if t == "string" then out[#out+1] = path .. " S " .. hex(v)
  elseif t == "number" then out[#out+1] = path .. " N " .. string.format("%.17g", v)
  elseif t == "boolean" then out[#out+1] = path .. " B " .. tostring(v)
  elseif t == "table" then
    out[#out+1] = path .. " T"
    for i = 1, #v do dump(path .. "[" .. i .. "]", v[i]) end
    for k, x in pairs(v) do
      if type(k) == "string" then dump(path .. "." .. hex(k), x)
      elseif not (type(k) == "number" and k >= 1 and k <= #v) then
        out[#out+1] = path .. " BADKEY " .. type(k)
      end
    end
  else out[#out+1] = path .. " BAD " .. t end
end
local f = assert(loadfile(arg[1]))
f()
dump("$", OmpcraftInbox)
table.sort(out)
io.write(table.concat(out, "\n"), "\n")
`

// Available returns the lua5.1 binary path, skipping the test when missing.
func Available(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("lua5.1")
	if err != nil {
		t.Skip("lua5.1 not on PATH")
	}
	return p
}

// Run loads data as a data.lua in lua5.1 and returns the sorted canonical dump
// lines of OmpcraftInbox.
func Run(t *testing.T, data []byte) []string {
	t.Helper()
	lua := Available(t)
	dir := t.TempDir()
	drv := filepath.Join(dir, "driver.lua")
	file := filepath.Join(dir, "data.lua")
	if err := os.WriteFile(drv, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(lua, drv, file).CombinedOutput()
	if err != nil {
		t.Fatalf("lua5.1 failed: %v\n%s\n--- data.lua ---\n%s", err, out, data)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(lines)
	return lines
}

// Expect dumps a generic Go tree (map[string]any, []any, string, bool, ints,
// floats) in the same format as Run. Strings, including keys, have "|" doubled,
// as the serializer's §5 escaping requires.
func Expect(v any) []string {
	var out []string
	expect("$", v, &out)
	sort.Strings(out)
	return out
}

func hexOf(s string) string { return fmt.Sprintf("%x", s) }

func pipes(s string) string { return strings.ReplaceAll(s, "|", "||") }

func expect(path string, v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, path+" S "+hexOf(pipes(x)))
	case bool:
		*out = append(*out, path+" B "+strconv.FormatBool(x))
	case int:
		*out = append(*out, path+" N "+fmt.Sprintf("%.17g", float64(x)))
	case int64:
		*out = append(*out, path+" N "+fmt.Sprintf("%.17g", float64(x)))
	case float64:
		*out = append(*out, path+" N "+fmt.Sprintf("%.17g", x))
	case []any:
		*out = append(*out, path+" T")
		for i, e := range x {
			expect(fmt.Sprintf("%s[%d]", path, i+1), e, out)
		}
	case map[string]any:
		*out = append(*out, path+" T")
		for k, e := range x {
			expect(path+"."+hexOf(pipes(k)), e, out)
		}
	default:
		panic(fmt.Sprintf("luatest.Expect: unsupported %T", v))
	}
}
