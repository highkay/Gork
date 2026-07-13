package tomlutil

import (
	"bytes"
	"strings"
	"testing"
)

func FuzzParseTOML(f *testing.F) {
	for _, seed := range []string{
		`app_url = "http://localhost:8000"`,
		"[cache.local]\nimage_max_mb = 256\nvideo_max_mb = 1024\n",
		"features = [\"stream\", \"webui\"]\n",
		"enabled = true # comment\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		data, err := Parse(strings.NewReader(raw))
		if err != nil {
			return
		}
		var out bytes.Buffer
		if err := Write(&out, data); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
		if _, err := Parse(bytes.NewReader(out.Bytes())); err != nil {
			t.Fatalf("roundtrip Parse returned error: %v", err)
		}
	})
}
