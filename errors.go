package specout

import (
	"slices"
	"strconv"
	"strings"
)

// Each panic and error of this package has one form:
//
//	specout: <where>: <problem>; <fix>
//
// where is first, so that a reader can find the code. It is a route such as
// "POST /jobs: response 200", or a Go name such as "Config.Auth". A value that
// the user wrote is quoted. The fix is present when specout knows it.

// didYouMean accepts two edits on a short name, and one edit in three characters
// on a long name.
const (
	minTypoDistance = 2
	typoCharacters  = 3
)

// didYouMean returns a hint with the name in known that is nearest to got, or
// an empty string when no name is near. The hint goes at the end of a message.
func didYouMean(got string, known []string) string {
	best, bestDistance := "", max(minTypoDistance, len(got)/typoCharacters)+1
	for _, name := range slices.Sorted(slices.Values(known)) {
		if d := editDistance(strings.ToLower(got), strings.ToLower(name)); d < bestDistance {
			best, bestDistance = name, d
		}
	}
	if best == "" {
		return ""
	}
	return "; did you mean " + strconv.Quote(best) + "?"
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := range len(a) {
		diagonal := row[0]
		row[0] = i + 1
		for j := range len(b) {
			cost := 1
			if a[i] == b[j] {
				cost = 0
			}
			diagonal, row[j+1] = row[j+1], min(row[j+1]+1, row[j]+1, diagonal+cost)
		}
	}
	return row[len(b)]
}

// addContext adds where to a specout panic that a deep function raised. Use it
// in a deferred function with the value of recover, around code that does not
// know the route. Another panic value keeps its type, because a caller can
// recover it.
func addContext(where string, v any) {
	if v == nil {
		return
	}
	if msg, ok := v.(string); ok {
		if rest, cut := strings.CutPrefix(msg, "specout: "); cut {
			panic("specout: " + where + ": " + rest)
		}
	}
	panic(v)
}
