package api

// yamlsubset_test.go -- a reader for the YAML subset openapi.yaml is written in (M11 API-12). The
// repository takes no YAML library (CLAUDE.md §1: a new dependency is asked for, and a test-only
// parser of a hand-written document does not need one), so the document is held to a SUBSET this
// file reads exactly, and anything outside it is an error, never a guess:
//
//	block mappings and block sequences, two-space indentation, no tabs
//	"- key: value" sequence items (a mapping that starts on the dash's line)
//	plain scalars (true, false, null, integers and decimals are typed; anything a YAML reader would
//	type otherwise -- True, NULL, 1e3, 0x10, +1, .inf, yes, 01, a date -- is an error; anything
//	else is a string),
//	double-quoted scalars (JSON escapes) and single-quoted scalars ('' is a quote)
//	literal block scalars ("|" and "|-")
//	flow collections that are JSON ({"a": [1, "x"]}, possibly over several lines) and flow
//	sequences of plain scalars ([employer, location])
//	whole-line comments
//
// Not read (an error if met): anchors, aliases, tags, folded scalars, flow mappings that are not
// JSON, trailing comments, multi-document streams, a byte-order mark. A mapping is ordered (ymap) so a test can say
// where in the document something is.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ymap is an ordered mapping.
type ymap struct {
	keys []string
	m    map[string]any
}

func newYmap() *ymap { return &ymap{m: map[string]any{}} }

func (y *ymap) get(k string) (any, bool) {
	v, ok := y.m[k]
	return v, ok
}

func (y *ymap) set(k string, v any) error {
	if _, dup := y.m[k]; dup {
		return fmt.Errorf("the key %q twice", k)
	}
	y.keys = append(y.keys, k)
	y.m[k] = v
	return nil
}

// yamlParser reads lines.
type yamlParser struct {
	lines []string
	i     int
}

// parseYAMLSubset reads one document of the subset.
func parseYAMLSubset(src string) (any, error) {
	if strings.Contains(src, "\t") {
		return nil, fmt.Errorf("a tab: the subset indents with spaces")
	}
	if strings.HasPrefix(src, "\uFEFF") {
		return nil, fmt.Errorf("a byte-order mark: the document starts with its first key")
	}
	p := &yamlParser{lines: strings.Split(strings.TrimSuffix(src, "\n"), "\n")}
	i, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("an empty document")
	}
	v, err := p.block(indentOf(p.lines[i]))
	if err != nil {
		return nil, err
	}
	if j, more := p.next(); more {
		return nil, fmt.Errorf("line %d: content after the document (%q)", j+1, p.lines[j])
	}
	return v, nil
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// next skips blank lines and whole-line comments and returns the index of the next significant line.
func (p *yamlParser) next() (int, bool) {
	for p.i < len(p.lines) {
		t := strings.TrimSpace(p.lines[p.i])
		if t == "" || strings.HasPrefix(t, "#") {
			p.i++
			continue
		}
		return p.i, true
	}
	return 0, false
}

func isSeqItem(text string) bool { return text == "-" || strings.HasPrefix(text, "- ") }

// block reads the node that starts at the next significant line, whose indentation is indent.
func (p *yamlParser) block(indent int) (any, error) {
	i, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("a missing node")
	}
	if isSeqItem(strings.TrimLeft(p.lines[i], " ")) {
		return p.sequence(indent)
	}
	return p.mapping(indent)
}

// keyShape is a mapping key and its colon: a double-quoted key, or a plain one that does not start
// with an indicator and holds no colon.
var keyShape = regexp.MustCompile(`^("[^"\\]*"|[^\s"'\[\]{},#&*!|>%@` + "`" + `-][^:]*|-[^\s:][^:]*):(\s+|$)`)

func (p *yamlParser) mapping(indent int) (any, error) {
	m := newYmap()
	for {
		i, ok := p.next()
		if !ok {
			return m, nil
		}
		line := p.lines[i]
		ind := indentOf(line)
		switch {
		case ind < indent:
			return m, nil
		case ind > indent:
			return nil, fmt.Errorf("line %d: unexpected indentation %d (the mapping is at %d)", i+1, ind, indent)
		}
		text := line[ind:]
		if isSeqItem(text) {
			return m, nil
		}
		k := keyShape.FindStringSubmatch(text)
		if k == nil {
			return nil, fmt.Errorf("line %d: not a key: %q", i+1, text)
		}
		key := k[1]
		if strings.HasPrefix(key, `"`) {
			key = key[1 : len(key)-1]
		}
		key = strings.TrimRight(key, " ")
		rest := strings.TrimSpace(text[len(k[0]):])
		p.i = i + 1
		v, err := p.value(rest, indent, i)
		if err != nil {
			return nil, err
		}
		if err := m.set(key, v); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
	}
}

func (p *yamlParser) sequence(indent int) (any, error) {
	out := []any{}
	for {
		i, ok := p.next()
		if !ok {
			return out, nil
		}
		line := p.lines[i]
		ind := indentOf(line)
		if ind < indent {
			return out, nil
		}
		text := line[ind:]
		if ind > indent || !isSeqItem(text) {
			if ind == indent {
				return out, nil
			}
			return nil, fmt.Errorf("line %d: not a sequence item at indentation %d: %q", i+1, indent, text)
		}
		item := strings.TrimSpace(strings.TrimPrefix(text, "-"))
		switch {
		case item == "":
			p.i = i + 1
			v, err := p.block(indent + 2)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		case keyShape.MatchString(item) && !strings.HasPrefix(item, "[") && !strings.HasPrefix(item, "{"):
			// "- key: value": a mapping whose first line is the dash's, two columns in.
			p.lines[i] = strings.Repeat(" ", indent+2) + item
			v, err := p.mapping(indent + 2)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		default:
			p.i = i + 1
			v, err := p.value(item, indent, i)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
}

// value reads what follows a key's colon (or a dash): nothing (a node below), a block scalar, a flow
// collection or a scalar. at is the line the value starts on.
func (p *yamlParser) value(rest string, indent, at int) (any, error) {
	switch {
	case rest == "":
		i, ok := p.next()
		if !ok {
			return nil, nil
		}
		ind := indentOf(p.lines[i])
		if ind > indent {
			return p.block(ind)
		}
		return nil, nil
	case rest == "|" || rest == "|-":
		return p.literal(indent, rest == "|-"), nil
	case strings.HasPrefix(rest, "[") || strings.HasPrefix(rest, "{"):
		text := rest
		for depth(text) > 0 {
			if p.i >= len(p.lines) {
				return nil, fmt.Errorf("line %d: an unclosed flow collection", at+1)
			}
			text += "\n" + p.lines[p.i]
			p.i++
		}
		if depth(text) < 0 {
			return nil, fmt.Errorf("line %d: an unbalanced flow collection", at+1)
		}
		return flow(text, at)
	default:
		return scalar(rest, at)
	}
}

// literal reads a "|" block: the lines indented past indent (and blank lines between them).
func (p *yamlParser) literal(indent int, strip bool) string {
	var body []string
	block := -1
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if strings.TrimSpace(line) == "" {
			body = append(body, "")
			p.i++
			continue
		}
		ind := indentOf(line)
		if ind <= indent {
			break
		}
		if block < 0 {
			block = ind
		}
		if ind < block {
			break
		}
		body = append(body, line[block:])
		p.i++
	}
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}
	s := strings.Join(body, "\n")
	if !strip && s != "" {
		s += "\n"
	}
	return s
}

// depth is the bracket depth of a flow text, outside double-quoted strings.
func depth(s string) int {
	d, in, esc := 0, false, false
	for _, c := range s {
		switch {
		case esc:
			esc = false
		case in && c == '\\':
			esc = true
		case c == '"':
			in = !in
		case !in && (c == '[' || c == '{'):
			d++
		case !in && (c == ']' || c == '}'):
			d--
		}
	}
	return d
}

// flow reads a flow collection: JSON, or a sequence of plain scalars.
func flow(text string, at int) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		if _, err := dec.Token(); err != nil {
			return fromJSON(v), nil
		}
		return nil, fmt.Errorf("line %d: text after a flow collection", at+1)
	}
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "[") || !strings.HasSuffix(t, "]") || strings.ContainsAny(t[1:len(t)-1], "[]{}") {
		return nil, fmt.Errorf("line %d: a flow collection that is neither JSON nor a list of plain scalars: %q", at+1, t)
	}
	out := []any{}
	inner := strings.TrimSpace(t[1 : len(t)-1])
	if inner == "" {
		return out, nil
	}
	for _, f := range strings.Split(inner, ",") {
		v, err := scalar(strings.TrimSpace(f), at)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// fromJSON turns a decoded JSON value into the subset's types: ordered maps (keys sorted -- JSON
// gives no order here) and float64 numbers.
func fromJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := newYmap()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			_ = m.set(k, fromJSON(x[k]))
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = fromJSON(x[i])
		}
		return out
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return v
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

var (
	plainInt   = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	plainFloat = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]+$`)
)

// scalar reads one scalar.
func scalar(s string, at int) (any, error) {
	switch {
	case s == "":
		return nil, fmt.Errorf("line %d: an empty scalar in a flow sequence", at+1)
	case strings.HasPrefix(s, `"`):
		var out string
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("line %d: a double-quoted scalar the subset does not read: %q", at+1, s)
		}
		return out, nil
	case strings.HasPrefix(s, "'"):
		if len(s) < 2 || !strings.HasSuffix(s, "'") {
			return nil, fmt.Errorf("line %d: an unclosed single-quoted scalar", at+1)
		}
		inner := s[1 : len(s)-1]
		if strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
			return nil, fmt.Errorf("line %d: a single quote inside a single-quoted scalar", at+1)
		}
		return strings.ReplaceAll(inner, "''", "'"), nil
	case strings.ContainsAny(s[:1], "&*!|>%@`?:,") || strings.HasPrefix(s, "- "):
		return nil, fmt.Errorf("line %d: an indicator the subset does not read: %q", at+1, s)
	case strings.Contains(s, ": ") || strings.HasSuffix(s, ":"):
		// A YAML reader refuses a plain scalar holding ": " (a mapping value is not allowed there).
		return nil, fmt.Errorf("line %d: a plain scalar with a colon and a space -- quote it: %q", at+1, s)
	case strings.Contains(s, " #"):
		return nil, fmt.Errorf("line %d: a trailing comment (the subset has whole-line comments only): %q", at+1, s)
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	case s == "null" || s == "~":
		return nil, nil
	case plainInt.MatchString(s) || plainFloat.MatchString(s):
		return strconv.ParseFloat(s, 64)
	case ambiguousPlain.MatchString(s):
		// A real reader types it (YAML 1.2's core schema: True, NULL, 1e3, 0x10, +1, .inf; or YAML
		// 1.1's: yes, on, 01, a date) where this subset would read a string: the document quotes it.
		return nil, fmt.Errorf("line %d: a plain scalar a YAML reader would not read as a string -- quote it: %q", at+1, s)
	}
	return s, nil
}

// ambiguousPlain is every plain scalar the subset would read as a string but a YAML reader as
// something else: the other spellings of a boolean and of null (YAML 1.2 core and 1.1), every number
// form but plainInt's and plainFloat's (a sign, a leading zero, an exponent, a trailing or leading
// dot, an underscore, 0x/0o/0b, .inf, .nan, sexagesimal) and a date.
var ambiguousPlain = regexp.MustCompile(`(?i)^(` +
	`true|false|null|yes|no|on|off|y|n` +
	`|[-+]?[0-9][0-9_]*(\.[0-9_]*)?(e[-+]?[0-9]+)?` +
	`|[-+]?\.[0-9][0-9_]*(e[-+]?[0-9]+)?` +
	`|[-+]?\.(inf|nan)` +
	`|0[xob][0-9a-f_]+` +
	`|[-+]?[0-9]+(:[0-5]?[0-9])+(\.[0-9_]*)?` +
	`|[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}([Tt ].*)?` +
	`)$`)

// jsonValue decodes JSON bytes into the subset's types (for comparing an answer with an example).
func jsonValue(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return fromJSON(v), nil
}

// canonical is a value's canonical JSON text (sorted keys): two values are equal exactly when their
// canonical texts are.
func canonical(v any) string {
	var plain func(v any) any
	plain = func(v any) any {
		switch x := v.(type) {
		case *ymap:
			m := make(map[string]any, len(x.keys))
			for _, k := range x.keys {
				m[k] = plain(x.m[k])
			}
			return m
		case []any:
			out := make([]any, len(x))
			for i := range x {
				out[i] = plain(x[i])
			}
			return out
		}
		return v
	}
	b, err := json.Marshal(plain(v))
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

// TestYAMLSubset_RefusesWhatAReaderWouldReadOtherwise (M11 API-12 fix): the subset never reads a
// value differently from a YAML reader. A plain scalar a reader types otherwise -- YAML 1.2's
// True, NULL, 1e3, 0x10, +1, .inf, .nan; YAML 1.1's yes, on, 01, a date, a sexagesimal -- a leading
// byte-order mark, and the forms the header lists as not read (an anchor, an alias, a tag, a folded
// scalar, a trailing comment) are each an error; the typed forms and the quoted ones are read.
//
// MUTANT (measured, the API-12 fix report): the ambiguousPlain case removed -- red (True, NULL, 1e3,
// 0x10, +1 and .inf read as strings).
func TestYAMLSubset_RefusesWhatAReaderWouldReadOtherwise(t *testing.T) {
	for _, doc := range []string{
		"a: True", "a: TRUE", "a: False", "a: NULL", "a: Null", "a: 1e3", "a: 1E3", "a: 0x10", "a: 0o17", "a: +1",
		"a: .inf", "a: -.Inf", "a: .nan", "a: .5", "a: 1.", "a: 01", "a: 1_000", "a: yes", "a: No", "a: on", "a: OFF",
		"a: 2026-10-16", "a: 1:20", "a: [True, b]", "\uFEFFa: b",
		"a: &x b", "a: *x", "a: !t b", "a: >\n  b", "a: b # c",
	} {
		if v, err := parseYAMLSubset(doc); err == nil {
			t.Errorf("%q read as %#v, want an error", doc, v)
		}
	}
	for doc, want := range map[string]any{
		"a: true": true, "a: false": false, "a: null": nil, "a: ~": nil, "a: 1000": 1000.0, "a: -2": -2.0, "a: 1.5": 1.5,
		`a: "True"`: "True", "a: 'NULL'": "NULL", `a: "1e3"`: "1e3", "a: text": "text", "a: v1.2.3": "v1.2.3",
		"a: application/json": "application/json",
	} {
		v, err := parseYAMLSubset(doc)
		if err != nil {
			t.Errorf("%q: %v", doc, err)
			continue
		}
		got, _ := v.(*ymap).get("a")
		if got != want {
			t.Errorf("%q read as %#v, want %#v", doc, got, want)
		}
	}
}
