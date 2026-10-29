// Package echopath translates Echo route paths into OpenAPI paths. Echo v4
// and v5 read route paths the same way, so both adapters use it. The scan
// follows Echo's walkRoutePath.
package echopath

import (
	"fmt"
	"strings"
)

// OpenAPI translates an Echo route path into OpenAPI syntax and lists the
// parameter names in Echo's order: /users/:id becomes /users/{id}, and an
// escaped \: is a literal colon, so /:id\:cancel becomes /{id}:cancel. A *
// matches the rest of the path, so it becomes the catch-all {*...}, named *,
// and Echo ignores anything after it.
func OpenAPI(p string) (string, []string, error) {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Echo's router adds the leading slash the same way
	}
	var b strings.Builder
	var names []string
	for i := 0; i < len(p); {
		switch {
		case isEscapedColon(p, i):
			b.WriteByte(':')
			i += 2
		case p[i] == ':':
			name, end, err := param(p, i+1)
			if err != nil {
				return "", nil, err
			}
			names = append(names, name)
			b.WriteString("{" + name + "}")
			i = end
		case p[i] == '*':
			b.WriteString("{*...}")
			return b.String(), append(names, "*"), nil
		default:
			static, end, err := staticText(p, i)
			if err != nil {
				return "", nil, err
			}
			b.WriteString(static)
			i = end
		}
	}
	return b.String(), names, nil
}

// Table translates a path from Echo's route table. A path OpenAPI rejects
// stays as Echo spells it, so a report still names it.
func Table(p string) string {
	full, _, err := OpenAPI(p)
	if err != nil {
		return p
	}
	return full
}

// param reads the parameter name that starts at start and returns the index
// after it.
func param(p string, start int) (string, int, error) {
	end := paramEnd(p, start)
	name := p[start:end]
	if name == "" || strings.ContainsAny(name, `{}:*\`) {
		return "", 0, fmt.Errorf("parameter name %q cannot be an OpenAPI parameter", name)
	}
	return name, end, nil
}

// staticText reads the literal text that starts at start, up to the next
// parameter, wildcard or escaped colon, and returns the index after it.
func staticText(p string, start int) (string, int, error) {
	i := start
	for i < len(p) && p[i] != ':' && p[i] != '*' && !isEscapedColon(p, i) {
		i++
	}
	static := p[start:i]
	if strings.ContainsAny(static, "{}") {
		return "", 0, fmt.Errorf("literal braces in %q read as OpenAPI parameters", static)
	}
	return static, i, nil
}

// paramEnd returns the end of the parameter name that starts at start. The
// name runs to the end of its segment, unless an escaped colon starts an
// inline verb such as \:cancel.
func paramEnd(p string, start int) int {
	end := segmentEnd(p, start)
	plain := true
	for i := start; i < end; i++ {
		if isEscapedColon(p, i) {
			if plain && isInlineVerb(p[i+2:]) {
				return i
			}
			return end
		}
		if p[i] == ':' || p[i] == '*' {
			plain = false
		}
	}
	return end
}

// segmentEnd returns the index of the first slash at or after i, or len(p).
func segmentEnd(p string, i int) int {
	if n := strings.IndexByte(p[i:], '/'); n >= 0 {
		return i + n
	}
	return len(p)
}

func isEscapedColon(p string, i int) bool {
	return p[i] == '\\' && i+1 < len(p) && p[i+1] == ':'
}

// isInlineVerb reports whether the text after an escaped colon stays static up
// to the end of its segment.
func isInlineVerb(rest string) bool {
	for i := 0; i < len(rest) && rest[i] != '/'; i++ {
		switch rest[i] {
		case '*':
			return false
		case ':':
			if i == 0 || rest[i-1] != '\\' {
				return false
			}
		}
	}
	return true
}
