package transport

import "testing"

func TestClampAssetConcurrency(t *testing.T) {
	if got := clampAssetConcurrency(0); got != 1 {
		t.Fatalf("clamp 0 = %d, want 1", got)
	}
	if got := clampAssetConcurrency(-3); got != 1 {
		t.Fatalf("clamp negative = %d, want 1", got)
	}
	if got := clampAssetConcurrency(50); got != 50 {
		t.Fatalf("clamp mid = %d, want 50", got)
	}
	if got := clampAssetConcurrency(maxAssetConcurrency + 10); got != maxAssetConcurrency {
		t.Fatalf("clamp high = %d, want %d", got, maxAssetConcurrency)
	}
}
