package openai

import "testing"

func TestValidRouterFileIDAcceptsHexAndSafeNames(t *testing.T) {
	if !validRouterFileID("1234567890abcdef") {
		t.Fatal("hex id should be valid")
	}
	if !validRouterFileID("generated-image_1") {
		t.Fatal("safe name should be valid")
	}
	if validRouterFileID("../secret") || validRouterFileID("bad/slash") || validRouterFileID("-bad-prefix") {
		t.Fatal("unsafe ids must be rejected")
	}
}
