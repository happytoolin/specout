package specout

import "strings"

// Document records an already-registered handler at an absolute pattern:
// the escape hatch for routers without an in-repo adapter (echo, fiber,
// gin). The handler must already be registered with the real router;
// specout only records the metadata.
func Document[Req, Res any](d *Generator, method, pattern string, h Handler[Req, Res]) {
	if !strings.HasPrefix(pattern, "/") {
		panic("specout: pattern must start with /, got " + pattern)
	}
	checkMethod(method)
	rec := recOf(h)
	rec.method, rec.pattern = method, pattern
	rec.full = pattern
	rec.absolute = true
	d.register(rec)
}
