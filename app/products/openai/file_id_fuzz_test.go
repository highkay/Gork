package openai

import "testing"

func FuzzValidRouterFileID(f *testing.F) {
	for _, seed := range []string{
		"1234567890abcdef",
		"12345678-1234-1234-1234-123456789abc",
		"../secret",
		"short",
		"zzzzzzzzzzzzzzzz",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, fileID string) {
		valid := validRouterFileID(fileID)
		if valid && (len(fileID) < 16 || len(fileID) > 36) {
			t.Fatalf("invalid length accepted: %q", fileID)
		}
		for _, char := range fileID {
			hex := char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char == '-'
			if valid && !hex {
				t.Fatalf("invalid character accepted: %q", fileID)
			}
		}
	})
}
