package specout_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/happytoolin/specout/internal/demoapp"
)

func TestGoldenSpec(t *testing.T) {
	var buf bytes.Buffer
	d, _, _ := demoapp.New()
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/openapi.json")
	if err != nil {
		t.Fatalf("golden missing (run: go test -update): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Error("spec differs from golden fixture")
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("UPDATE_GOLDEN") != "" {
		var buf bytes.Buffer
		d, _, _ := demoapp.New()
		if err := d.WriteJSON(&buf); err != nil {
			panic(err)
		}
		os.MkdirAll("testdata", 0o755)
		os.WriteFile("testdata/openapi.json", buf.Bytes(), 0o644)
	}
	os.Exit(m.Run())
}
