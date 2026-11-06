package specout

import (
	jsonv1 "encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/invopop/jsonschema"
)

// checkExtrasTag panics on a jsonschema_extras key that has a checked form. An
// extra goes into the schema as written, so exclusiveMinimum=true or
// required=true there would break the document. where names the field.
func checkExtrasTag(where, tag string) {
	for _, part := range tagParts(tag) {
		key, _, _ := strings.Cut(part, "=")
		switch {
		case key == "nullable":
			panic("specout: " + where + ": jsonschema_extras key nullable is an OpenAPI 3.0 keyword; " +
				`use a pointer field, or jsonschema:"nullable"`)
		case slices.Contains(schemaTagKeys, key) || slices.Contains(schemaTagFlags, key):
			panic("specout: " + where + ": jsonschema_extras key " + strconv.Quote(key) +
				` has a checked form; write it in the jsonschema tag, as jsonschema:"` + key + `=..."`)
		}
	}
}

// fixFieldValues fixes and checks the values that the tags of a field put in
// its schema p. where names the field.
func fixFieldValues(where string, p *jsonschema.Schema) {
	if p == nil {
		return
	}
	convertListDefault(where, p)
	for s := p; s != nil; s = s.Items {
		checkKeywordValues(where, s)
	}
	if p.Default != nil {
		checkValue(where, "default", p.Default, p)
	}
	for _, example := range p.Examples {
		checkValue(where, "example", example, p)
	}
}

// convertListDefault converts the items of a list default to the item type.
// The reflector keeps each item of default=1|2 on a []int as the text "1".
func convertListDefault(where string, p *jsonschema.Schema) {
	items, ok := p.Default.([]any)
	if !ok || p.Items == nil {
		return
	}
	for i, item := range items {
		text, isText := item.(string)
		if !isText {
			continue
		}
		switch p.Items.Type {
		case typeInteger, typeNumber:
			n, isNumber := jsonNumber(text)
			if !isNumber {
				panic("specout: " + where + ": default item " + strconv.Quote(text) + " is not a number; fix the default= tag")
			}
			items[i] = n
		case typeBoolean:
			b, err := strconv.ParseBool(text)
			if err != nil {
				panic("specout: " + where + ": default item " + strconv.Quote(text) + " is not true or false; fix the default= tag")
			}
			items[i] = b
		}
	}
}

// anchorRe is the form of a $anchor in JSON Schema 2020-12.
var anchorRe = regexp.MustCompile(`^[A-Za-z_][-A-Za-z0-9._]*$`)

// badPattern lists the regular expression errors that every dialect has. Go
// rejects some ECMA-262 forms, such as a lookahead, that the document allows.
var badPattern = []syntax.ErrorCode{
	syntax.ErrMissingBracket, syntax.ErrMissingParen, syntax.ErrUnexpectedParen, syntax.ErrTrailingBackslash,
	syntax.ErrMissingRepeatArgument, syntax.ErrInvalidRepeatOp, syntax.ErrInvalidCharRange,
}

// checkKeywordValues panics on a keyword value that JSON Schema 2020-12 does
// not allow in s.
func checkKeywordValues(where string, s *jsonschema.Schema) {
	if s.Anchor != "" && !anchorRe.MatchString(s.Anchor) {
		panic("specout: " + where + ": anchor " + strconv.Quote(s.Anchor) + " is not valid; " +
			"start it with a letter or _, and use only letters, digits, -, _ and .")
	}
	checkPattern(where, s.Pattern)
	if s.MultipleOf != "" {
		if n, err := s.MultipleOf.Float64(); err == nil && n <= 0 {
			panic("specout: " + where + ": multipleOf=" + s.MultipleOf.String() + " is not valid; use a number above 0")
		}
	}
	for i, v := range s.Enum {
		if slices.ContainsFunc(s.Enum[:i], func(e any) bool { return sameJSON(e, v) }) {
			panic("specout: " + where + ": enum value " + jsonText(v) + " is repeated; list each value once")
		}
	}
}

// checkPattern panics when pattern is not a regular expression in any dialect.
func checkPattern(where, pattern string) {
	if pattern == "" {
		return
	}
	var serr *syntax.Error
	if _, err := syntax.Parse(pattern, syntax.Perl); errors.As(err, &serr) && slices.Contains(badPattern, serr.Code) {
		panic("specout: " + where + ": pattern " + strconv.Quote(pattern) + " is not a regular expression: " +
			string(serr.Code) + `; fix the pattern= tag, and write a comma in it as \\,`)
	}
}

// checkValue panics when v, the default or an example of s, is not valid
// against s.
func checkValue(where, what string, v any, s *jsonschema.Schema) {
	if problem := valueProblem(decodeJSON(v), s); problem != "" {
		panic("specout: " + where + ": " + what + " " + jsonText(v) + " " + problem + "; fix the " + what + "= tag")
	}
}

// valueProblem says why the decoded JSON value v is not valid against s, or
// returns "". It checks the keywords that a tag sets: the type, enum, const,
// and the limits of a string, a number and a list.
func valueProblem(v any, s *jsonschema.Schema) string {
	if s == nil || s.Ref != "" {
		return ""
	}
	if problem := anyValueProblem(v, s); problem != "" {
		return problem
	}
	switch v := v.(type) {
	case string:
		return stringProblem(v, s)
	case jsonv1.Number:
		return numberProblem(v, s)
	case []any:
		return listProblem(v, s)
	}
	return ""
}

// anyValueProblem checks the keywords that apply to a value of each type: the
// type, enum and const.
func anyValueProblem(v any, s *jsonschema.Schema) string {
	types := schemaTypeNames(s)
	switch {
	case len(types) > 0 && !slices.ContainsFunc(types, func(t string) bool { return hasType(v, t) }):
		return "is not of type " + strings.Join(types, " or ")
	case len(s.Enum) > 0 && !slices.ContainsFunc(s.Enum, func(e any) bool { return sameJSON(e, v) }):
		return "is not in the enum " + jsonText(s.Enum)
	case s.Const != nil && !sameJSON(s.Const, v):
		return "is not the const " + jsonText(s.Const)
	}
	return ""
}

func stringProblem(v string, s *jsonschema.Schema) string {
	n := uint64(utf8.RuneCountInString(v)) //nolint:gosec // a rune count is not negative
	switch {
	case s.MinLength != nil && n < *s.MinLength:
		return "is shorter than minLength " + strconv.FormatUint(*s.MinLength, 10)
	case s.MaxLength != nil && n > *s.MaxLength:
		return "is longer than maxLength " + strconv.FormatUint(*s.MaxLength, 10)
	}
	if re, err := regexp.Compile(s.Pattern); s.Pattern != "" && err == nil && !re.MatchString(v) {
		return "does not match the pattern " + strconv.Quote(s.Pattern)
	}
	return ""
}

func numberProblem(v jsonv1.Number, s *jsonschema.Schema) string {
	x, err := v.Float64()
	if err != nil {
		return ""
	}
	limit := func(n jsonv1.Number, fails func(x, n float64) bool) bool {
		bound, boundErr := n.Float64()
		return n != "" && boundErr == nil && fails(x, bound)
	}
	switch {
	case limit(s.Minimum, func(x, n float64) bool { return x < n }):
		return "is below the minimum " + s.Minimum.String()
	case limit(s.Maximum, func(x, n float64) bool { return x > n }):
		return "is above the maximum " + s.Maximum.String()
	case limit(s.ExclusiveMinimum, func(x, n float64) bool { return x <= n }):
		return "is not above the exclusiveMinimum " + s.ExclusiveMinimum.String()
	case limit(s.ExclusiveMaximum, func(x, n float64) bool { return x >= n }):
		return "is not below the exclusiveMaximum " + s.ExclusiveMaximum.String()
	case limit(s.MultipleOf, func(x, n float64) bool { return n > 0 && math.Abs(math.Remainder(x, n)) > 1e-9 }):
		return "is not a multiple of " + s.MultipleOf.String()
	}
	return ""
}

func listProblem(v []any, s *jsonschema.Schema) string {
	n := uint64(len(v))
	switch {
	case s.MinItems != nil && n < *s.MinItems:
		return "has fewer items than minItems " + strconv.FormatUint(*s.MinItems, 10)
	case s.MaxItems != nil && n > *s.MaxItems:
		return "has more items than maxItems " + strconv.FormatUint(*s.MaxItems, 10)
	}
	for i, item := range v {
		if problem := valueProblem(item, s.Items); problem != "" {
			return "has item " + strconv.Itoa(i) + " " + jsonText(item) + ", which " + problem
		}
	}
	return ""
}

// schemaTypeNames is the type of s: its type, or the list of types that a
// nullable schema keeps in Extras.
func schemaTypeNames(s *jsonschema.Schema) []string {
	switch types := s.Extras["type"].(type) {
	case []string:
		return types
	case []any:
		var names []string
		for _, t := range types {
			if name, ok := t.(string); ok {
				names = append(names, name)
			}
		}
		return names
	}
	if s.Type != "" {
		return []string{s.Type}
	}
	return nil
}

// hasType reports whether the decoded JSON value v has the JSON Schema type t.
// A number with no fraction is an integer.
func hasType(v any, t string) bool {
	switch v := v.(type) {
	case nil:
		return t == nullType
	case bool:
		return t == typeBoolean
	case string:
		return t == typeString
	case []any:
		return t == typeArray
	case map[string]any:
		return t == typeObject
	case jsonv1.Number:
		if t == typeNumber {
			return true
		}
		x, err := v.Float64()
		return t == typeInteger && err == nil && x == math.Trunc(x)
	}
	return false
}

// decodeJSON is v as encoding/json writes it, decoded with numbers kept exact.
func decodeJSON(v any) any {
	data, err := jsonv1.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	decoder := jsonv1.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if decoder.Decode(&out) != nil {
		return v
	}
	return out
}

// sameJSON reports whether a and b encode to the same JSON value.
func sameJSON(a, b any) bool { return jsonText(decodeJSON(a)) == jsonText(decodeJSON(b)) }

func jsonText(v any) string {
	data, err := jsonv1.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

// checkRefs fails when a $ref in the document has no target. A $ref that starts
// with # must point into the document. A $ref that is only a name, such as
// oneof_ref=Pet, names nothing.
func checkRefs(document []byte) error {
	var doc any
	if err := jsonv1.Unmarshal(document, &doc); err != nil {
		return fmt.Errorf("specout: read the document for its $ref values: %w", err)
	}
	bad := map[string]error{}
	walkJSON(doc, func(ref string) {
		pointer, local := strings.CutPrefix(ref, "#")
		switch {
		case local && !resolves(doc, pointer):
			bad[ref] = fmt.Errorf("specout: $ref %q points to nothing in the document; "+
				"point a Raw $ref at a part of the document, and a oneof_ref or anyof_ref tag "+
				"at a type that a route uses, as #/$defs/<Type>", ref)
		case !local && !strings.ContainsAny(ref, "/.:#"):
			bad[ref] = fmt.Errorf("specout: $ref %q is a name, not a reference; write #/$defs/%s", ref, ref)
		}
	})
	errs := make([]error, 0, len(bad))
	for _, ref := range sortedKeys(bad) {
		errs = append(errs, bad[ref])
	}
	return errors.Join(errs...)
}

// walkJSON calls fn with the value of each $ref string in v.
func walkJSON(v any, fn func(ref string)) {
	switch v := v.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			fn(ref)
		}
		for _, child := range v {
			walkJSON(child, fn)
		}
	case []any:
		for _, child := range v {
			walkJSON(child, fn)
		}
	}
}

func resolves(doc any, pointer string) bool {
	if pointer == "" {
		return true
	}
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	node := doc
	for token := range strings.SplitSeq(pointer[1:], "/") {
		var ok bool
		if node, ok = pointerStep(node, strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")); !ok {
			return false
		}
	}
	return true
}

func pointerStep(node any, token string) (any, bool) {
	switch n := node.(type) {
	case map[string]any:
		child, ok := n[token]
		return child, ok
	case []any:
		i, err := strconv.Atoi(token)
		if err != nil || i < 0 || i >= len(n) {
			return nil, false
		}
		return n[i], true
	}
	return nil, false
}

// isJSONSchemaer reports whether t has a JSONSchema method, which gives its
// schema.
func isJSONSchemaer(t reflect.Type) bool {
	iface := reflect.TypeFor[interface{ JSONSchema() *jsonschema.Schema }]()
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}
