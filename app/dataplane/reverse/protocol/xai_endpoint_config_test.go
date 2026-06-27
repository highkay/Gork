package protocol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type fakeProtocolConfigBackend struct {
	data map[string]any
}

func (f fakeProtocolConfigBackend) Load(context.Context) (map[string]any, error) {
	return f.data, nil
}

func (f fakeProtocolConfigBackend) ApplyPatch(context.Context, map[string]any) error {
	return nil
}

func (f fakeProtocolConfigBackend) Clear(context.Context) error {
	return nil
}

func (f fakeProtocolConfigBackend) Version(context.Context) (any, error) {
	return "test", nil
}

func (f fakeProtocolConfigBackend) Close(context.Context) error {
	return nil
}

func useProtocolGlobalConfig(t *testing.T, endpoints map[string]any) {
	t.Helper()
	previous := platformconfig.GlobalConfig
	t.Cleanup(func() { platformconfig.GlobalConfig = previous })
	defaultsPath := filepath.Join(t.TempDir(), "config.defaults.toml")
	if err := os.WriteFile(defaultsPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	platformconfig.GlobalConfig = platformconfig.NewConfigSnapshot(fakeProtocolConfigBackend{
		data: map[string]any{"reverse": map[string]any{"endpoints": endpoints}},
	}, platformconfig.ConfigSnapshotOptions{})
	if err := platformconfig.GlobalConfig.Load(context.Background(), defaultsPath); err != nil {
		t.Fatalf("load global config: %v", err)
	}
}

func TestGrokImageURLUsesConfiguredAssetsEndpoint(t *testing.T) {
	useProtocolGlobalConfig(t, map[string]any{"assets_cdn": "https://assets.test"})

	if got, want := grokImageURL("/images/file.png"), "https://assets.test/images/file.png"; got != want {
		t.Fatalf("grokImageURL = %q, want %q", got, want)
	}
}

func TestLiveKitPayloadUsesConfiguredWebSocketEndpoint(t *testing.T) {
	useProtocolGlobalConfig(t, map[string]any{"ws_livekit": "wss://voice.test"})

	outer := decodeJSONMap(t, BuildLiveKitTokenRequestPayload(LiveKitTokenOptions{}))
	if got, want := outer["livekitUrl"], "wss://voice.test"; got != want {
		t.Fatalf("livekitUrl = %#v, want %q", got, want)
	}
}
