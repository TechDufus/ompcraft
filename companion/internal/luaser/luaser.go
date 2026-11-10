// Package luaser serializes Go values into a data-only Lua 5.1 assignment:
// tables, strings, numbers and booleans, never code (PROTOCOL §5).
package luaser

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxDepth = 32

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Adapted from t3code-wow (MIT): bridge/src/luaser.js (IDENT/RESERVED key rule).
// "goto" is reserved too: it is a keyword in Lua 5.2+.
var reserved = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true,
	"false": true, "for": true, "function": true, "goto": true, "if": true, "in": true,
	"local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true,
	"then": true, "true": true, "until": true, "while": true,
}

// Marshal returns `name = <value>\n`. Struct fields need a `lua:"key"` tag
// (`lua:"-"` skips a field). Struct fields keep declaration order; map keys
// (strings only) are sorted. Nil pointers and nil interfaces in struct fields
// or maps are omitted; nil slices become empty tables. A field tagged
// `lua:"key,omitempty"` is also omitted when it is the zero value or an empty
// slice or map.
func Marshal(name string, v any) ([]byte, error) {
	if !identRe.MatchString(name) || reserved[name] {
		return nil, fmt.Errorf("luaser: invalid global name %q", name)
	}
	var sb strings.Builder
	sb.WriteString(name)
	sb.WriteString(" = ")
	if err := value(&sb, reflect.ValueOf(v), 0); err != nil {
		return nil, err
	}
	sb.WriteByte('\n')
	return []byte(sb.String()), nil
}

// String returns the escaped, quoted Lua string literal for s (PROTOCOL §5):
// printable ASCII is literal except `\` and `"`; `\\`, `\"` and `\n` are
// escaped; `|` is doubled; every other byte is `\ddd`.
func String(s string) string {
	var sb strings.Builder
	writeString(&sb, s)
	return sb.String()
}

func writeString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '"':
			sb.WriteString(`\"`)
		case c == '\\':
			sb.WriteString(`\\`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '|':
			sb.WriteString("||")
		case c >= 0x20 && c <= 0x7e:
			sb.WriteByte(c)
		default:
			sb.WriteByte('\\')
			sb.WriteString(fmt.Sprintf("%03d", c))
		}
	}
	sb.WriteByte('"')
}

func writeKey(sb *strings.Builder, k string) {
	if identRe.MatchString(k) && !reserved[k] {
		sb.WriteString(k)
		return
	}
	sb.WriteByte('[')
	writeString(sb, k)
	sb.WriteByte(']')
}

func indent(sb *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		sb.WriteString("  ")
	}
}

func nilable(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func value(sb *strings.Builder, v reflect.Value, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("luaser: nesting deeper than %d", maxDepth)
	}
	if !v.IsValid() {
		sb.WriteString("nil")
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			sb.WriteString("nil")
			return nil
		}
		return value(sb, v.Elem(), depth)
	case reflect.Bool:
		sb.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		sb.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if u > math.MaxInt64 {
			return fmt.Errorf("luaser: integer %d out of int64 range", u)
		}
		sb.WriteString(strconv.FormatUint(u, 10))
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("luaser: cannot serialize %v", f)
		}
		sb.WriteString(strconv.FormatFloat(f, 'g', -1, v.Type().Bits()))
	case reflect.String:
		writeString(sb, v.String())
	case reflect.Slice, reflect.Array:
		return array(sb, v, depth)
	case reflect.Map:
		return mapValue(sb, v, depth)
	case reflect.Struct:
		return structValue(sb, v, depth)
	default:
		return fmt.Errorf("luaser: unsupported type %s", v.Type())
	}
	return nil
}

func array(sb *strings.Builder, v reflect.Value, depth int) error {
	if v.Len() == 0 {
		sb.WriteString("{}")
		return nil
	}
	sb.WriteString("{\n")
	for i := 0; i < v.Len(); i++ {
		indent(sb, depth+1)
		if err := value(sb, v.Index(i), depth+1); err != nil {
			return err
		}
		sb.WriteString(",\n")
	}
	indent(sb, depth)
	sb.WriteByte('}')
	return nil
}

func mapValue(sb *strings.Builder, v reflect.Value, depth int) error {
	if v.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("luaser: map keys must be strings, got %s", v.Type().Key())
	}
	keys := make([]string, 0, v.Len())
	byKey := make(map[string]reflect.Value, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		if nilable(iter.Value()) {
			continue
		}
		k := iter.Key().String()
		keys = append(keys, k)
		byKey[k] = iter.Value()
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		sb.WriteString("{}")
		return nil
	}
	sb.WriteString("{\n")
	for _, k := range keys {
		indent(sb, depth+1)
		writeKey(sb, k)
		sb.WriteString(" = ")
		if err := value(sb, byKey[k], depth+1); err != nil {
			return err
		}
		sb.WriteString(",\n")
	}
	indent(sb, depth)
	sb.WriteByte('}')
	return nil
}

func structValue(sb *strings.Builder, v reflect.Value, depth int) error {
	t := v.Type()
	sb.WriteString("{\n")
	n := 0
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("lua")
		if !ok {
			return fmt.Errorf("luaser: field %s.%s has no lua tag", t, f.Name)
		}
		if tag == "-" {
			continue
		}
		key, opts, _ := strings.Cut(tag, ",")
		omitEmpty := opts == "omitempty"
		fv := v.Field(i)
		if (fv.Kind() == reflect.Pointer || fv.Kind() == reflect.Interface) && fv.IsNil() {
			continue
		}
		if omitEmpty && fv.IsZero() || omitEmpty && (fv.Kind() == reflect.Slice || fv.Kind() == reflect.Map) && fv.Len() == 0 {
			continue
		}
		indent(sb, depth+1)
		writeKey(sb, key)
		sb.WriteString(" = ")
		if err := value(sb, fv, depth+1); err != nil {
			return err
		}
		sb.WriteString(",\n")
		n++
	}
	if n == 0 {
		return fmt.Errorf("luaser: struct %s has no serializable fields", t)
	}
	indent(sb, depth)
	sb.WriteByte('}')
	return nil
}
