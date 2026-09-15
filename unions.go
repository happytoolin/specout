package specout

import "reflect"

// Register declares T a union variant under name, for oneof_type tags on
// Variant discriminator fields. Call before building the spec.
func (d *Generator) Register[T any](name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frozen {
		panic("specout: registration after the spec was built")
	}
	d.variants[name] = reflect.TypeFor[T]()
}

// SchemaName overrides the component name generated for T — the fix when two
// packages own types with the same name. Call before building the spec.
func (d *Generator) SchemaName[T any](name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frozen {
		panic("specout: registration after the spec was built")
	}
	d.nameOverrides[reflect.TypeFor[T]()] = name
}
