package openai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
)

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
	if client.calls != 2 {
		t.Fatalf("ProbeListModels should bypass cache, calls=%d", client.calls)
	}
	request := client.requests[0]
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
	body     []byte
	err      error
	status   int
	calls    int
	requests []*http.Request
}

func (c *fakeDynamicModelHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.calls++
	c.requests = append(c.requests, request)
	if c.err != nil {
		return nil, c.err
	}
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/grpc-web+proto"}},
		Body:       io.NopCloser(bytes.NewReader(c.body)),
	}, nil
}
