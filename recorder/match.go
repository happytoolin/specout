package recorder

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/happytoolin/specout"
)

// matchRoute returns the documented route that serves a request for method and
// the escaped path. When several templates match, the one with a literal
// segment first, from the left, wins, as in http.ServeMux and chi. A HEAD
// request with no HEAD route goes to the GET route.
func matchRoute(routes statuses, method, path string) (specout.RouteKey, bool) {
	segments := strings.Split(path, "/")
	var best specout.RouteKey
	var bestRank []int
	for key := range routes {
		if key.Method != method {
			continue
		}
		if rank, ok := matchTemplate(strings.Split(key.Path, "/"), segments); ok &&
			(bestRank == nil || slices.Compare(rank, bestRank) > 0 ||
				(slices.Compare(rank, bestRank) == 0 && key.Path < best.Path)) {
			best, bestRank = key, rank
		}
	}
	if bestRank == nil && method == http.MethodHead {
		return matchRoute(routes, http.MethodGet, path)
	}
	return best, bestRank != nil
}

// The rank of one template segment: a literal beats a segment that mixes text
// and a parameter, which beats a parameter, which beats a catch-all.
const (
	rankCatchAll = iota
	rankParam
	rankMixed
	rankLiteral
)

// matchTemplate reports whether the segments of a path template match the
// segments of a path, and ranks each template segment. A last segment * or
// {name...} matches the rest of the path.
func matchTemplate(template, path []string) ([]int, bool) {
	rank := make([]int, 0, len(template))
	for i, t := range template {
		if i == len(template)-1 && (t == "*" || strings.HasSuffix(t, "...}")) {
			return append(rank, rankCatchAll), len(path) >= len(template)
		}
		if i >= len(path) {
			return nil, false
		}
		r, ok := matchSegment(t, path[i])
		if !ok {
			return nil, false
		}
		rank = append(rank, r)
	}
	return rank, len(path) == len(template)
}

// templateParam matches one {name} parameter in a template segment.
var templateParam = regexp.MustCompile(`\{[^{}]*\}`)

// matchSegment matches one template segment against one escaped path segment.
func matchSegment(template, segment string) (int, bool) {
	value, err := url.PathUnescape(segment)
	if err != nil {
		value = segment
	}
	switch {
	case !strings.Contains(template, "{"):
		return rankLiteral, template == value
	case templateParam.FindString(template) == template:
		return rankParam, segment != ""
	}
	literals := templateParam.Split(template, -1)
	for i, literal := range literals {
		literals[i] = regexp.QuoteMeta(literal)
	}
	re := regexp.MustCompile("^" + strings.Join(literals, "[^/]+") + "$")
	return rankMixed, re.MatchString(value)
}
