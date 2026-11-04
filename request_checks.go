package specout

import (
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// checkURL panics when u, the value of field, is not a URL. OpenAPI requires
// the form of a URL in each field that this checks. An empty u is not set.
func checkURL(where, field, u string) {
	if u == "" {
		return
	}
	if _, err := url.Parse(u); err != nil || strings.IndexFunc(u, unicode.IsSpace) >= 0 {
		panic("specout: " + where + ": " + field + " " + strconv.Quote(u) + " is not a URL; " +
			"write a URL such as https://example.com/docs")
	}
}

// checkEmail panics when e is not an email address, which OpenAPI requires in
// the contact email.
func checkEmail(where, e string) {
	if e == "" {
		return
	}
	if addr, err := mail.ParseAddress(e); err != nil || addr.Address != e {
		panic("specout: " + where + ": Email " + strconv.Quote(e) + " is not an email address; " +
			"write only the address, such as api@example.com")
	}
}

// pathCharProblem returns why the path p has a character that a request path
// cannot carry, or "". A ? starts the query and a # the fragment, so the
// route would never match.
func pathCharProblem(p string) string {
	i := strings.IndexFunc(p, func(r rune) bool {
		return r == '?' || r == '#' || unicode.IsSpace(r) || unicode.IsControl(r)
	})
	if i < 0 {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(p[i:])
	return "the path has the character " + strconv.QuoteRune(r) +
		", which a request path cannot carry; put query values in Req fields with a query tag"
}

// checkMediaTypes panics on a media type that is not type/subtype, such as
// "json", and on one that the list repeats. Media types ignore case.
func checkMediaTypes(where string, types []string) {
	seen := make([]string, 0, len(types))
	for _, ct := range types {
		if ct == "" {
			panic("specout: " + where + ": ContentTypes has an empty media type")
		}
		base, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.Contains(base, "/") {
			panic("specout: " + where + ": media type " + strconv.Quote(ct) + " is not valid; " +
				"write type/subtype, such as application/json")
		}
		key := strings.ToLower(strings.ReplaceAll(ct, " ", ""))
		if slices.Contains(seen, key) {
			panic("specout: " + where + ": media type " + strconv.Quote(ct) + " is repeated; media types ignore case")
		}
		seen = append(seen, key)
	}
}

// ignoredHeaders are the header parameters that OpenAPI ignores, and where
// the document describes them instead.
var ignoredHeaders = map[string]string{
	"accept":        "Responses and their ContentTypes",
	"content-type":  "Request.ContentTypes",
	"authorization": "Config.Auth and Handler.Security",
}

// checkHeaderParam panics on a header parameter that OpenAPI ignores.
func checkHeaderParam(f reflect.StructField, name string) {
	if instead, ok := ignoredHeaders[strings.ToLower(name)]; ok {
		panic("specout: field " + f.Name + ": OpenAPI ignores a header parameter named " + strconv.Quote(name) +
			"; describe it with " + instead + ", and remove the header tag")
	}
}

// styleDeepObject is the query style that writes the keys of an object.
const styleDeepObject = "deepObject"

// styleKinds are the Go kinds that a parameter style serializes. A style
// that OpenAPI does not define for the type of the value is undefined.
var styleKinds = map[string][]reflect.Kind{
	styleDeepObject:  {reflect.Struct, reflect.Map},
	"spaceDelimited": {reflect.Slice, reflect.Array, reflect.Struct, reflect.Map},
	"pipeDelimited":  {reflect.Slice, reflect.Array, reflect.Struct, reflect.Map},
}

// checkStyleKind panics when the style of a parameter does not fit its type,
// or when deepObject is not exploded.
func checkStyleKind(f reflect.StructField, style string, explode *bool) {
	kinds, ok := styleKinds[style]
	if !ok {
		return
	}
	if kind := deref(f.Type).Kind(); !slices.Contains(kinds, kind) {
		want := "a slice, an array, a struct or a map"
		if style == styleDeepObject {
			want = "a struct or a map"
		}
		panic("specout: field " + f.Name + ": style " + style + " does not fit a " + kind.String() + "; use it on " + want)
	}
	if style == styleDeepObject && explode != nil && !*explode {
		panic("specout: field " + f.Name + ": style deepObject needs explode=true; OpenAPI does not define it without")
	}
}

// checkBodyFiles panics on a request body that cannot carry its files. A file
// list needs multipart/form-data, and a struct with a file field needs a
// multipart media type.
func checkBodyFiles(rec *routeRecord) {
	body, _ := requestBody(rec.req)
	if body == nil || body == reflect.TypeFor[File]() {
		return
	}
	t := deref(body)
	if (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) && deref(t.Elem()) == reflect.TypeFor[File]() {
		panic("specout: " + rec.label() + ": a JSON body cannot carry files; " +
			"make Req a struct with a field such as Files []specout.File, for multipart/form-data")
	}
	if !hasBinaryField(body) {
		return
	}
	for _, ct := range rec.body.ContentTypes {
		if !strings.HasPrefix(baseMediaType(ct), "multipart/") {
			panic("specout: " + rec.label() + ": Request.ContentTypes: " + ct + " cannot carry the file fields of the body; " +
				"use a multipart media type, or remove ContentTypes for multipart/form-data")
		}
	}
}

// checkTraceBody panics on a TRACE operation with a request body. RFC 9110
// says that a client must not send content in a TRACE request.
func checkTraceBody(rec *routeRecord) {
	if body, _ := requestBody(rec.req); body != nil && rec.method == http.MethodTrace {
		panic("specout: " + rec.label() + ": a TRACE request has no body; remove the body fields from Req")
	}
}

// operationTags returns the tags of an operation, each once. An empty tag
// names no group, so it panics.
func operationTags(where string, tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" {
			panic("specout: " + where + ": Tags has an empty tag; remove it")
		}
		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}
