package storage

import (
	"fmt"
	"testing"
)

func BenchmarkLocalMediaCacheReconcile(b *testing.B) {
	b.Setenv("DATA_DIR", b.TempDir())
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{
		"cache.local.image_max_mb": 64,
	}})
	raw := []byte("image-bytes")
	for i := 0; i < 1000; i++ {
		if _, err := store.SaveImage(raw, "image/png", fmt.Sprintf("bench-%04d", i)); err != nil {
			b.Fatalf("SaveImage returned error: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Reconcile(MediaTypeImage); err != nil {
			b.Fatalf("Reconcile returned error: %v", err)
		}
	}
}
