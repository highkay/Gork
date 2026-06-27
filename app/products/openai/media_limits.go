package openai

import (
	"fmt"
	"io"

	"github.com/dslzl/gork/app/platform"
)

const (
	maxLocalImageAssetBytes int64 = 64 << 20
	maxLocalVideoAssetBytes int64 = 512 << 20
)

func readLocalMediaAsset(reader io.Reader, maxBytes int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, platform.NewUpstreamError(fmt.Sprintf("upstream media asset exceeds %d bytes", maxBytes), 502, "")
	}
	return raw, nil
}
