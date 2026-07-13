package openai

import (
	"errors"
	"strings"
	"testing"

	"github.com/dslzl/gork/app/platform"
)

func TestReadLocalMediaAssetRejectsOversizedSuccessfulStream(t *testing.T) {
	raw, err := readLocalMediaAsset(strings.NewReader("1234"), 4)
	if err != nil || string(raw) != "1234" {
		t.Fatalf("readLocalMediaAsset within limit raw=%q err=%v", raw, err)
	}
	_, err = readLocalMediaAsset(strings.NewReader("12345"), 4)
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 502 {
		t.Fatalf("oversized err=%#v", err)
	}
}

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
