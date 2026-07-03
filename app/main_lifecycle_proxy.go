package app

import (
	"context"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	proxycontrol "github.com/dslzl/gork/app/control/proxy"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
)

func defaultAppMainStartProxyScheduler(ctx context.Context, _ *appMainLifecycleState) (Hook, error) {
	if !accountcontrol.IsRefreshSchedulerLeader() {
		return nil, nil
	}
	directory, err := proxycontrol.GetProxyDirectory(ctx, proxydataplane.ProductionDirectoryOptions())
	if err != nil {
		return nil, err
	}
	scheduler := proxycontrol.NewProxyClearanceScheduler(directory)
	scheduler.Start(ctx)
	return func(context.Context) error {
		scheduler.Stop()
		return nil
	}, nil
}
