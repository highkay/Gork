package proxy

import controlproxy "github.com/dslzl/gork/app/control/proxy"

type BundleKey struct {
	AffinityKey   string
	ClearanceHost string
}

type ProxyRuntimeTable struct {
	EgressMode    controlproxy.EgressMode
	ClearanceMode controlproxy.ClearanceMode
	Nodes         []controlproxy.EgressNode
	Bundles       map[BundleKey]controlproxy.ClearanceBundle
}

type ProxyDirectorySnapshot interface {
	EgressMode() controlproxy.EgressMode
	ClearanceMode() controlproxy.ClearanceMode
	Nodes() []controlproxy.EgressNode
	Bundles() map[BundleKey]controlproxy.ClearanceBundle
}

func NewProxyRuntimeTable() ProxyRuntimeTable {
	return ProxyRuntimeTable{
		EgressMode:    controlproxy.EgressModeDirect,
		ClearanceMode: controlproxy.ClearanceModeNone,
		Nodes:         []controlproxy.EgressNode{},
		Bundles:       map[BundleKey]controlproxy.ClearanceBundle{},
	}
}

func (t ProxyRuntimeTable) NodeCount() int {
	return len(t.Nodes)
}

func (t ProxyRuntimeTable) HasNodes() bool {
	return len(t.Nodes) > 0
}

func (t ProxyRuntimeTable) HealthyNodes() []controlproxy.EgressNode {
	healthy := make([]controlproxy.EgressNode, 0, len(t.Nodes))
	for _, node := range t.Nodes {
		if node.State == controlproxy.EgressNodeHealthy {
			healthy = append(healthy, node)
		}
	}
	return healthy
}

func SnapshotFromDirectory(directory ProxyDirectorySnapshot) ProxyRuntimeTable {
	return ProxyRuntimeTable{
		EgressMode:    directory.EgressMode(),
		ClearanceMode: directory.ClearanceMode(),
		Nodes:         cloneEgressNodes(directory.Nodes()),
		Bundles:       cloneClearanceBundles(directory.Bundles()),
	}
}

func cloneEgressNodes(nodes []controlproxy.EgressNode) []controlproxy.EgressNode {
	out := make([]controlproxy.EgressNode, len(nodes))
	copy(out, nodes)
	for i := range out {
		out[i].ProxyURL = cloneStringPtr(out[i].ProxyURL)
		out[i].LastUsed = cloneInt64Ptr(out[i].LastUsed)
	}
	return out
}

func cloneClearanceBundles(bundles map[BundleKey]controlproxy.ClearanceBundle) map[BundleKey]controlproxy.ClearanceBundle {
	if bundles == nil {
		return nil
	}
	out := make(map[BundleKey]controlproxy.ClearanceBundle, len(bundles))
	for key, bundle := range bundles {
		bundle.LastRefreshAt = cloneInt64Ptr(bundle.LastRefreshAt)
		bundle.ExpiresAt = cloneInt64Ptr(bundle.ExpiresAt)
		out[key] = bundle
	}
	return out
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
