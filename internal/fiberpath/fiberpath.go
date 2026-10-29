// Package fiberpath translates Fiber route paths into OpenAPI paths. Fiber v2
// and v3 read route paths the same way, so both adapters use it.
package fiberpath

import (
	"fmt"
	"strconv"
	"strings"
)

// OpenAPI translates a Fiber route path into OpenAPI syntax and lists the
// parameter names in Fiber's order: /users/:id becomes /users/{id}, and
// /:id<int> drops its constraint, since the Req field types the parameter. A
// wildcard * or + becomes a catch-all such as {*1...}; Fiber names them *1,
// *2 and +1, +2 in order. A backslash makes the next character literal, and
// so does a doubled colon.
//
// An optional parameter (:id?) matches two paths, and OpenAPI has no optional
// path parameter, so it is an error: register both paths instead. Any other
// syntax this scan does not know is an error too, rather than a guess.
func OpenAPI(p string) (string, []string, error) {
	s := scan{p: p, wildcards: make(map[byte]int)}
	for i := 0; i < len(p); {
		next, err := s.token(i)
		if err != nil {
			return "", nil, err
		}
		i = next
	}
	return s.b.String(), s.names, nil
}

// Table translates a path from Fiber's route table. A path OpenAPI rejects
// stays as Fiber spells it, so a report still names it.
func Table(p string) string {
	full, _, err := OpenAPI(p)
	if err != nil {
		return p
	}
	return full
}

// Join is Fiber's own join of a group prefix and a route path, with the
// leading slash Fiber adds to a bare path. An empty path keeps the prefix as
// written, trailing slash included.
func Join(prefix, p string) string {
	full := prefix
	if p != "" {
		if p[0] != '/' {
			p = "/" + p
		}
		full = strings.TrimRight(prefix, "/") + p
	}
	if full == "" || full[0] != '/' {
		full = "/" + full
	}
	return full
}

// scan is the state of one OpenAPI translation.
type scan struct {
	p         string
	b         strings.Builder
	names     []string
	wildcards map[byte]int // '*' and '+' -> how many came before
}

// token translates the token that starts at i and returns the index after it.
func (s *scan) token(i int) (int, error) {
	switch c := s.p[i]; c {
	case '\\':
		return s.escape(i)
	case ':':
		return s.colon(i)
	case '*', '+':
		s.wildcards[c]++
		s.param(string(c)+strconv.Itoa(s.wildcards[c]), "...")
	case '{', '}':
		return 0, s.bracesError()
	default:
		s.b.WriteByte(c)
	}
	return i + 1, nil
}

// escape writes the character after the backslash at i as a literal.
func (s *scan) escape(i int) (int, error) {
	lit := i + 1
	if lit == len(s.p) {
		return 0, fmt.Errorf("trailing backslash in %q", s.p)
	}
	if c := s.p[lit]; c == '{' || c == '}' {
		return 0, s.bracesError()
	}
	s.b.WriteByte(s.p[lit])
	return lit + 1, nil
}

// colon translates the parameter that starts at i. A doubled colon is a
// literal colon before a parameter: color::color matches color:blue.
func (s *scan) colon(i int) (int, error) {
	if i+1 < len(s.p) && s.p[i+1] == ':' {
		s.b.WriteByte(':')
		return i + 1, nil
	}
	name, end, err := param(s.p, i+1)
	if err != nil {
		return 0, err
	}
	s.param(name, "")
	return end, nil
}

// param records a parameter and writes it in OpenAPI syntax.
func (s *scan) param(name, suffix string) {
	s.names = append(s.names, name)
	s.b.WriteString("{" + name + suffix + "}")
}

func (s *scan) bracesError() error {
	return fmt.Errorf("literal braces in %q read as OpenAPI parameters", s.p)
}

// param reads the parameter name that starts at start, with its optional
// <constraint>, and returns the index after it. A name ends at a slash, a
// dash, a dot, another parameter or an escape, as in Fiber's parser.
func param(p string, start int) (string, int, error) {
	i := start
	for i < len(p) && !strings.ContainsRune(`/-.:\?<*+{}`, rune(p[i])) {
		i++
	}
	name := p[start:i]
	if name == "" {
		return "", 0, fmt.Errorf("empty parameter name in %q", p)
	}
	if i < len(p) && p[i] == '<' {
		end := constraintEnd(p, i+1)
		if end < 0 {
			return "", 0, fmt.Errorf("unclosed constraint on :%s in %q", name, p)
		}
		i = end + 1
	}
	if i < len(p) {
		switch p[i] {
		case '?':
			return "", 0, fmt.Errorf("optional parameter :%s? matches two paths; register each path", name)
		case '*', '+', '{', '}':
			return "", 0, fmt.Errorf("parameter :%s is followed by %q, which specout cannot read", name, p[i])
		}
	}
	return name, i, nil
}

// constraintEnd returns the index of the > that closes a constraint opened
// before start, skipping escaped characters, or -1.
func constraintEnd(p string, start int) int {
	for i := start; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '>':
			return i
		}
	}
	return -1
}
