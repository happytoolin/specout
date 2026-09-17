package specout_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoldenSpec(t *testing.T) {
	var buf bytes.Buffer
	d, _ := router.New()
	require.NoError(t, d.WriteJSON(&buf))
	golden, err := os.ReadFile("testdata/openapi.json")
	require.NoError(t, err, "golden missing (run: just golden)")
	assert.True(t, bytes.Equal(buf.Bytes(), golden), "spec differs from golden fixture")
}

func TestMain(m *testing.M) {
	if os.Getenv("UPDATE_GOLDEN") != "" {
		var buf bytes.Buffer
		d, _ := router.New()
		if err := d.WriteJSON(&buf); err != nil {
			panic(err)
		}
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			panic(err)
		}
		if err := os.WriteFile("testdata/openapi.json", buf.Bytes(), 0o600); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
