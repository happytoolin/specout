package specout

import "reflect"

// mustBeOpen panics when the spec was already built, so no registration can
// slip into a frozen document. Caller holds d.mu.
func (d *Generator) mustBeOpen() {
	if d.frozen {
		panic("specout: registration after the spec was built")
	}
}

// Register declares T a union variant under name, for oneof_type tags on
// Variant discriminator fields. Call before building the spec.
func (d *Generator) Register[T any](name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mustBeOpen()
	d.variants[name] = reflect.TypeFor[T]()
}

// SchemaName overrides the component name generated for T — the fix when two
// packages own types with the same name. Call before building the spec.
func (d *Generator) SchemaName[T any](name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mustBeOpen()
	d.nameOverrides[reflect.TypeFor[T]()] = name
}
