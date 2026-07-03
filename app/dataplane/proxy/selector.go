package proxy

import controlproxy "github.com/dslzl/gork/app/control/proxy"

func SelectProxy(table ProxyRuntimeTable) *string {
	if table.EgressMode == controlproxy.EgressModeDirect {
		return nil
	}

	if table.EgressMode == controlproxy.EgressModeSingleProxy {
		if len(table.Nodes) > 0 {
			return table.Nodes[0].ProxyURL
		}
		return nil
	}

	healthy := table.HealthyNodes()
	if len(healthy) == 0 {
		return nil
	}

	best := healthy[0]
	for _, node := range healthy[1:] {
		if node.Inflight < best.Inflight {
			best = node
		}
	}
	return best.ProxyURL
}
