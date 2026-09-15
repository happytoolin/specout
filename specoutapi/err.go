package specoutapi

// DetailedError bypasses the generic mapper: the domain error knows its own
// status and wire shape.
type DetailedError interface {
	error
	HTTPStatus() int
	Payload() any
}
