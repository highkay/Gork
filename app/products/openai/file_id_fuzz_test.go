package openai

import "testing"

func FuzzValidRouterFileID(f *testing.F) {
	for _, seed := range []string{
		"1234567890abcdef",
		"12345678-1234-1234-1234-123456789abc",
		"generated-image_1",
		"../secret",
		"bad/slash",
		"-bad-prefix",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, fileID string) {
		valid := validRouterFileID(fileID)
		if valid && !routerFileIDRE.MatchString(fileID) && !routerSafeFileIDRE.MatchString(fileID) {
			t.Fatalf("invalid file ID accepted: %q", fileID)
		}
	})
}
