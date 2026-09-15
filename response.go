package specout

// Response declares one HTTP status code for an operation. Status is
// required; the rest is optional per the merge rules in the API reference.
type Response struct {
	Status  int
	Type    any
	Headers []Header
	Raw     map[string]any
	Omit    bool
}

// Header declares a named response header.
type Header struct {
	Name string
	Type any
}

// NoContent as Res means 204, no body.
type NoContent struct{}

// File as a request field means multipart/form-data + binary content.
type File struct{}

// Binary as Res means a binary response body.
type Binary struct{}

// Variant is a discriminator field value, paired with a oneof_type tag.
type Variant string
