package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type fakeDynamicConfigBackend struct {
	data map[string]any
}

func (f fakeDynamicConfigBackend) Load(context.Context) (map[string]any, error) {
	return f.data, nil
}

func (f fakeDynamicConfigBackend) ApplyPatch(context.Context, map[string]any) error {
	return nil
}

func (f fakeDynamicConfigBackend) Clear(context.Context) error {
	return nil
}

func (f fakeDynamicConfigBackend) Version(context.Context) (any, error) {
	return "test", nil
}

func (f fakeDynamicConfigBackend) Close(context.Context) error {
	return nil
}

func useDynamicGlobalConfig(t *testing.T, data map[string]any) {
	t.Helper()
	previous := platformconfig.GlobalConfig
	t.Cleanup(func() { platformconfig.GlobalConfig = previous })
	defaultsPath := filepath.Join(t.TempDir(), "config.defaults.toml")
	if err := os.WriteFile(defaultsPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	platformconfig.GlobalConfig = platformconfig.NewConfigSnapshot(fakeDynamicConfigBackend{data: data}, platformconfig.ConfigSnapshotOptions{})
	if err := platformconfig.GlobalConfig.Load(context.Background(), defaultsPath); err != nil {
		t.Fatalf("load global config: %v", err)
	}
}

func TestDynamicConsoleModelRegistryDelegatesSource(t *testing.T) {
	source := &fakeDynamicConsoleModelProvider{
		specs:  []model.ModelSpec{{ModelName: "grok-dynamic"}},
		status: DynamicConsoleModelStatus{CachedModels: 1},
		err:    errors.New("probe failed"),
	}
	registry := newDynamicConsoleModelRegistry(source)

	got := registry.ListContext(context.Background())
	if !reflect.DeepEqual(got, source.specs) {
		t.Fatalf("ListContext = %#v, want %#v", got, source.specs)
	}
	if status := registry.Status(); status.CachedModels != 1 {
		t.Fatalf("Status = %#v", status)
	}
	err := registry.ProbeListModels(context.Background(), "probe-token")
	if err == nil || err.Error() != "probe failed" {
		t.Fatalf("ProbeListModels error = %v", err)
	}
	if source.probedToken != "probe-token" {
		t.Fatalf("probed token = %q", source.probedToken)
	}
}

func TestDynamicConsoleModelSourceUsesConfiguredEndpointAtRequestTime(t *testing.T) {
	useDynamicGlobalConfig(t, map[string]any{
		"reverse": map[string]any{
			"endpoints": map[string]any{
				"console_base": "https://console.test",
			},
		},
	})
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Client: client,
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})

	if got := modelNamesForSpecs(source.ListContext(context.Background())); len(got) == 0 {
		t.Fatalf("configured endpoint list empty")
	}
	if client.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1", client.requestCount())
	}
	want := "https://console.test/auth_mgmt.AuthManagement/ListModels"
	if got := client.requestAt(0).URL.String(); got != want {
		t.Fatalf("request URL = %q, want %q", got, want)
	}
}

func TestDynamicConsoleModelSourceFetchesAndCachesListModels(t *testing.T) {
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Endpoint: "https://console.x.ai/auth_mgmt.AuthManagement/ListModels",
		TTL:      time.Minute,
		Now:      func() time.Time { return time.Unix(100, 0) },
		Client: &fakeDynamicModelHTTPClient{
			body: sampleDynamicConsoleModelsResponse(t),
		},
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})

	got := source.List()
	names := modelNamesForSpecs(got)
	want := []string{"grok-4.20-dynamic", "grok-4.20-dynamic-latest", "grok-code-fast"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("dynamic names = %#v, want %#v", names, want)
	}
	for _, spec := range got {
		if spec.ModeID != model.ModeConsole || spec.Tier != model.TierBasic || spec.Capability != model.CapabilityConsoleChat || !spec.Enabled {
			t.Fatalf("dynamic spec = %#v", spec)
		}
	}

	got = source.List()
	if source.client.(*fakeDynamicModelHTTPClient).calls != 1 {
		t.Fatalf("List should use cache on second call, calls=%d got=%#v", source.client.(*fakeDynamicModelHTTPClient).calls, got)
	}

	request := source.client.(*fakeDynamicModelHTTPClient).requests[0]
	if request.Method != http.MethodPost || request.URL.String() != "https://console.x.ai/auth_mgmt.AuthManagement/ListModels" {
		t.Fatalf("request target = %s %s", request.Method, request.URL.String())
	}
	if request.Header.Get("Content-Type") != "application/grpc-web+proto" ||
		request.Header.Get("x-grpc-web") != "1" ||
		request.Header.Get("x-user-agent") == "" ||
		request.Header.Get("Authorization") != "Bearer anonymous" {
		t.Fatalf("request headers = %#v", request.Header)
	}
	if cookie := request.Header.Get("Cookie"); cookie != "sso=sso-token; sso-rw=sso-token" {
		t.Fatalf("request cookie = %q", cookie)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if !reflect.DeepEqual(body, transport.EncodeGRPCWebPayload(nil)) {
		t.Fatalf("request body = %#v, want empty grpc-web frame", body)
	}
}

func TestDynamicConsoleModelSourceRejectsOversizedListModelsBody(t *testing.T) {
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Endpoint: "https://console.x.ai/auth_mgmt.AuthManagement/ListModels",
		Client: &fakeDynamicModelHTTPClient{
			body: bytes.Repeat([]byte("x"), int(maxDynamicConsoleListModelsResponseBytes)+1),
		},
	})

	_, _, err := source.postListModels(context.Background(), "sso-token")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("postListModels error = %v, want oversized body error", err)
	}
}

func TestDynamicConsoleModelSourceFallsBackToCachedModelsOnRefreshFailure(t *testing.T) {
	now := time.Unix(100, 0)
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Endpoint: "https://console.x.ai/auth_mgmt.AuthManagement/ListModels",
		TTL:      time.Second,
		Now:      func() time.Time { return now },
		Client:   client,
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})

	if got := modelNamesForSpecs(source.List()); len(got) == 0 {
		t.Fatalf("initial list is empty")
	}
	now = now.Add(2 * time.Second)
	client.err = context.DeadlineExceeded
	if got := modelNamesForSpecs(source.List()); !reflect.DeepEqual(got, []string{"grok-4.20-dynamic", "grok-4.20-dynamic-latest", "grok-code-fast"}) {
		t.Fatalf("refresh failure should return stale cache, got %#v", got)
	}
}

func TestDynamicConsoleModelSourceStatusTracksCacheAndRefreshResults(t *testing.T) {
	now := time.Unix(100, 0)
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Endpoint: "https://console.x.ai/auth_mgmt.AuthManagement/ListModels",
		TTL:      time.Second,
		Now:      func() time.Time { return now },
		Client:   client,
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})

	source.List()
	source.List()
	now = now.Add(2 * time.Second)
	client.err = errors.New("refresh failed")
	source.List()
	waitForDynamicRefresh(t, func() bool {
		return source.Status().RefreshFailures == 1
	})

	status := source.Status()
	if status.CacheHits != 1 || status.CacheMisses != 2 || status.RefreshSuccesses != 1 || status.RefreshFailures != 1 {
		t.Fatalf("status = %#v", status)
	}
	if status.CachedModels != 3 || status.LastSuccessAt.IsZero() || status.LastFailureAt.IsZero() || status.LastError == "" {
		t.Fatalf("status timestamps/cache = %#v", status)
	}
}

func TestDynamicConsoleModelSourceListContextCancelsInitialRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Client: client,
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})

	if got := source.ListContext(ctx); len(got) != 0 {
		t.Fatalf("cancelled initial list = %#v, want empty fallback", got)
	}
	if client.callCount() != 0 {
		t.Fatalf("cancelled context should not start HTTP refresh, calls=%d", client.callCount())
	}
	if source.LastErrorTime().IsZero() {
		t.Fatalf("cancelled refresh should expose last error time")
	}
}

func TestDynamicConsoleModelSourceReturnsStaleCacheWhileRefreshing(t *testing.T) {
	now := time.Unix(100, 0)
	release := make(chan struct{})
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		TTL:    time.Second,
		Now:    func() time.Time { return now },
		Client: client,
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})
	if got := modelNamesForSpecs(source.ListContext(context.Background())); len(got) == 0 {
		t.Fatalf("initial list is empty")
	}

	now = now.Add(2 * time.Second)
	client.setBlock(release)
	got := modelNamesForSpecs(source.ListContext(context.Background()))
	if !reflect.DeepEqual(got, []string{"grok-4.20-dynamic", "grok-4.20-dynamic-latest", "grok-code-fast"}) {
		t.Fatalf("stale list = %#v", got)
	}
	waitForDynamicRefresh(t, func() bool { return client.callCount() == 2 })
	if client.callCount() != 2 {
		t.Fatalf("expired cache should start one background refresh, calls=%d", client.callCount())
	}
	close(release)
}

func TestDynamicConsoleModelSourceKeepsStaleCacheAndRecordsError(t *testing.T) {
	now := time.Unix(100, 0)
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		TTL:    time.Second,
		Now:    func() time.Time { return now },
		Client: client,
		Directory: func() chatDirectory {
			return &fakeChatDirectory{accounts: []chatAccount{{Token: "sso-token", ModeID: model.ModeConsole}}}
		},
	})
	if got := modelNamesForSpecs(source.ListContext(context.Background())); len(got) == 0 {
		t.Fatalf("initial list is empty")
	}

	now = now.Add(2 * time.Second)
	client.err = errors.New("account unavailable")
	if got := modelNamesForSpecs(source.ListContext(context.Background())); !reflect.DeepEqual(got, []string{"grok-4.20-dynamic", "grok-4.20-dynamic-latest", "grok-code-fast"}) {
		t.Fatalf("refresh failure should return stale cache, got %#v", got)
	}
	waitForDynamicRefresh(t, func() bool { return !source.LastErrorTime().IsZero() })
	if got := modelNamesForSpecs(source.ListContext(context.Background())); !reflect.DeepEqual(got, []string{"grok-4.20-dynamic", "grok-4.20-dynamic-latest", "grok-code-fast"}) {
		t.Fatalf("failed background refresh should keep stale cache, got %#v", got)
	}
}

func TestDynamicConsoleModelSourceNoAccountReturnsEmptyWithoutBlockingModels(t *testing.T) {
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Client: &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)},
		Directory: func() chatDirectory {
			return &fakeChatDirectory{}
		},
	})

	if got := source.ListContext(context.Background()); len(got) != 0 {
		t.Fatalf("no-account list = %#v, want empty degradation", got)
	}
	if source.LastErrorTime().IsZero() {
		t.Fatalf("no-account degradation should expose last error time")
	}
}

func TestDynamicConsoleModelSourceProbeListModelsBypassesCache(t *testing.T) {
	client := &fakeDynamicModelHTTPClient{body: sampleDynamicConsoleModelsResponse(t)}
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Endpoint: "https://console.x.ai/auth_mgmt.AuthManagement/ListModels",
		TTL:      time.Hour,
		Now:      func() time.Time { return time.Unix(100, 0) },
		Client:   client,
	})
	source.cache = []model.ModelSpec{{ModelName: "cached"}}
	source.expiresAt = time.Unix(200, 0)

	if err := source.ProbeListModels(context.Background(), "probe-token"); err != nil {
		t.Fatalf("ProbeListModels returned error: %v", err)
	}
	if err := source.ProbeListModels(context.Background(), "probe-token"); err != nil {
		t.Fatalf("ProbeListModels second call returned error: %v", err)
	}
	if client.callCount() != 2 {
		t.Fatalf("ProbeListModels should bypass cache, calls=%d", client.callCount())
	}
	request := client.requestAt(0)
	if cookie := request.Header.Get("Cookie"); cookie != "sso=probe-token; sso-rw=probe-token" {
		t.Fatalf("probe cookie = %q", cookie)
	}
}

func TestDynamicConsoleModelSourceProbeListModelsFailsOnHTTPStatus(t *testing.T) {
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Client: &fakeDynamicModelHTTPClient{status: http.StatusForbidden, body: []byte("blocked-user")},
	})

	err := source.ProbeListModels(context.Background(), "bad-token")

	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("ProbeListModels error = %v", err)
	}
}

func TestDynamicConsoleModelSourceProbeListModelsFailsOnGRPCStatus(t *testing.T) {
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Client: &fakeDynamicModelHTTPClient{body: grpcWebTrailer("7", "denied")},
	})

	err := source.ProbeListModels(context.Background(), "bad-token")

	if err == nil || !strings.Contains(err.Error(), "grpc status=7") {
		t.Fatalf("ProbeListModels error = %v", err)
	}
}

func TestDynamicConsoleModelSourceProbeListModelsFailsOnNetworkError(t *testing.T) {
	source := newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		Client: &fakeDynamicModelHTTPClient{err: errors.New("network down")},
	})

	err := source.ProbeListModels(context.Background(), "bad-token")

	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("ProbeListModels error = %v", err)
	}
}

func sampleDynamicConsoleModelsResponse(t *testing.T) []byte {
	t.Helper()
	modelOne := protoMessage(
		protoStringField(1, "grok-4.20-dynamic"),
		protoStringField(2, "1.0"),
		protoStringField(14, "grok-4.20-dynamic-latest"),
		protoStringField(14, "grok-imagine-image"),
	)
	modelTwo := protoMessage(
		protoStringField(1, "grok-code-fast"),
		protoStringField(2, "1.0"),
	)
	region := protoMessage(
		protoBytesField(1, modelOne),
		protoBytesField(1, modelTwo),
		protoStringField(3, "us-east-1"),
	)
	return transport.EncodeGRPCWebPayload(protoMessage(protoBytesField(1, region)))
}

func grpcWebTrailer(status string, message string) []byte {
	return grpcWebFrame(0x80, []byte("grpc-status: "+status+"\r\ngrpc-message: "+message+"\r\n"))
}

func grpcWebFrame(flag byte, payload []byte) []byte {
	frame := []byte{flag, 0, 0, 0, 0}
	frame[1] = byte(len(payload) >> 24)
	frame[2] = byte(len(payload) >> 16)
	frame[3] = byte(len(payload) >> 8)
	frame[4] = byte(len(payload))
	return append(frame, payload...)
}

func protoMessage(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func protoStringField(number int, value string) []byte {
	return protoBytesField(number, []byte(value))
}

func protoBytesField(number int, value []byte) []byte {
	out := protoVarint(uint64(number<<3 | 2))
	out = append(out, protoVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func protoVarint(value uint64) []byte {
	out := []byte{}
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}

func modelNamesForSpecs(specs []model.ModelSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.ModelName)
	}
	return names
}

type fakeDynamicModelHTTPClient struct {
	mu       sync.Mutex
	body     []byte
	err      error
	status   int
	calls    int
	block    <-chan struct{}
	requests []*http.Request
}

type fakeDynamicConsoleModelProvider struct {
	specs       []model.ModelSpec
	status      DynamicConsoleModelStatus
	err         error
	probedToken string
}

func (p *fakeDynamicConsoleModelProvider) ListContext(context.Context) []model.ModelSpec {
	return p.specs
}

func (p *fakeDynamicConsoleModelProvider) Status() DynamicConsoleModelStatus {
	return p.status
}

func (p *fakeDynamicConsoleModelProvider) ProbeListModels(_ context.Context, token string) error {
	p.probedToken = token
	if p.err != nil {
		return fmt.Errorf("%w", p.err)
	}
	return nil
}

func (c *fakeDynamicModelHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.calls++
	c.requests = append(c.requests, request)
	block := c.block
	err := c.err
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	body := slices.Clone(c.body)
	c.mu.Unlock()
	if block != nil {
		<-block
	}
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/grpc-web+proto"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

func (c *fakeDynamicModelHTTPClient) setBlock(block <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.block = block
}

func (c *fakeDynamicModelHTTPClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *fakeDynamicModelHTTPClient) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func (c *fakeDynamicModelHTTPClient) requestAt(index int) *http.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[index]
}

func waitForDynamicRefresh(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background dynamic refresh did not finish before deadline")
}
