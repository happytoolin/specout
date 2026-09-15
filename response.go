package specout

// Response declares one HTTP status code for an operation.
//
// Type omitted = inherit the handler Res type at this code. ContentType set
// = a binary/raw body (schema format binary), e.g. "application/pdf".
// Raw splices arbitrary OpenAPI fragments into the response object,
// including response headers: Raw: map[string]any{"headers": ...}.
type Response struct {
	Status      int
	Type        any
	ContentType string
	Raw         map[string]any
	Omit        bool
}
