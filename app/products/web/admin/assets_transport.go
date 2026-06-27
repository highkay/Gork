package admin

import (
	"context"

	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
)

func defaultAdminListAssets(ctx context.Context, token string) (map[string]any, error) {
	runtime, err := proxydataplane.GetTransportRuntime(ctx)
	if err != nil {
		return nil, err
	}
	return transport.ListAssets(ctx, token, nil, transport.AssetsOptions{ProxyRuntime: runtime})
}

func defaultAdminDeleteAsset(ctx context.Context, token string, assetID string) error {
	runtime, err := proxydataplane.GetTransportRuntime(ctx)
	if err != nil {
		return err
	}
	_, err = transport.DeleteAsset(ctx, token, assetID, transport.AssetsOptions{ProxyRuntime: runtime})
	return err
}
