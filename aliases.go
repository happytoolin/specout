package specout

// Read-shape aliases: named spellings of the most common Handler shapes.
// All are full type aliases, not new types — registration, reflection and
// the With* chain see Handler itself, and every construct that takes a
// Handler accepts these directly.

// Get is the empty-request read: no body, no params, Res as the 200 body.
type Get[Res any] = Handler[struct{}, Res]

// Delete is the shape with nothing in and nothing out: the operation answers
// 204. Responses can add an error, such as a 404, and the 204 stays.
type Delete = Handler[struct{}, NoContent]

// Event is the shape of a webhook or a callback whose receiver answers with no
// body: Body is what your API sends, and the receiver answers 204. A declared
// success response, such as Key: "2XX", replaces the 204.
type Event[Body any] = Handler[Body, NoContent]
