package openai

import (
	"context"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
)

func defaultProxyTransportRuntime(ctx context.Context) (*controlproxy.ProxyDirectory, error) {
	return proxydataplane.GetTransportRuntime(ctx)
}
