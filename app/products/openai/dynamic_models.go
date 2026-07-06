package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dslzl/gork/app/control/model"
	controlproxy "github.com/dslzl/gork/app/control/proxy"
	proxyadapters "github.com/dslzl/gork/app/dataplane/proxy/adapters"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
)

const maxDynamicConsoleListModelsResponseBytes int64 = 8 << 20

var defaultDynamicConsoleModels = newDynamicConsoleModelRegistry(
	newDynamicConsoleModelSource(dynamicConsoleModelSourceOptions{
		TTL:        10 * time.Minute,
		FailureTTL: 30 * time.Second,
	}),
)

func init() {
	model.SetDynamicProviderContext(defaultDynamicConsoleModels.ListContext)
}

type dynamicConsoleHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type dynamicConsoleModelProvider interface {
	ListContext(context.Context) []model.ModelSpec
	Status() DynamicConsoleModelStatus
	ProbeListModels(context.Context, string) error
}

type dynamicConsoleModelSourceOptions struct {
	Endpoint   string
	TTL        time.Duration
	FailureTTL time.Duration
	Now        func() time.Time
	Client     dynamicConsoleHTTPClient
	Directory  func() chatDirectory
}

type dynamicConsoleModelSource struct {
	mu         sync.Mutex
	endpoint   string
	ttl        time.Duration
	failureTTL time.Duration
	now        func() time.Time
	client     dynamicConsoleHTTPClient
	directory  func() chatDirectory
	cache      []model.ModelSpec
	expiresAt  time.Time
	refreshing bool
	lastErrAt  time.Time
	lastErr    string

	cacheHits        int
	cacheMisses      int
	refreshSuccesses int
	refreshFailures  int
	lastSuccessAt    time.Time
	lastFailureAt    time.Time
}

type DynamicConsoleModelStatus struct {
	CachedModels     int       `json:"cached_models"`
	CacheHits        int       `json:"cache_hits"`
	CacheMisses      int       `json:"cache_misses"`
	RefreshSuccesses int       `json:"refresh_successes"`
	RefreshFailures  int       `json:"refresh_failures"`
	Refreshing       bool      `json:"refreshing"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	LastSuccessAt    time.Time `json:"last_success_at,omitempty"`
	LastFailureAt    time.Time `json:"last_failure_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
}

type dynamicConsoleModelRegistry struct {
	provider dynamicConsoleModelProvider
}

func newDynamicConsoleModelRegistry(provider dynamicConsoleModelProvider) *dynamicConsoleModelRegistry {
	return &dynamicConsoleModelRegistry{provider: provider}
}

func (r *dynamicConsoleModelRegistry) ListContext(ctx context.Context) []model.ModelSpec {
	if r.provider == nil {
		return []model.ModelSpec{}
	}
	return r.provider.ListContext(ctx)
}

func (r *dynamicConsoleModelRegistry) Status() DynamicConsoleModelStatus {
	if r.provider == nil {
		return DynamicConsoleModelStatus{}
	}
	return r.provider.Status()
}

func (r *dynamicConsoleModelRegistry) ProbeListModels(ctx context.Context, token string) error {
	if r.provider == nil {
		return errors.New("dynamic console model provider is not configured")
	}
	return r.provider.ProbeListModels(ctx, token)
}

func newDynamicConsoleModelSource(options dynamicConsoleModelSourceOptions) *dynamicConsoleModelSource {
	endpoint := options.Endpoint
	ttl := options.TTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	failureTTL := options.FailureTTL
	if failureTTL <= 0 {
		failureTTL = 30 * time.Second
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	client := options.Client
	directory := options.Directory
	if directory == nil {
		directory = chatDirectoryProvider
	}
	return &dynamicConsoleModelSource{
		endpoint: endpoint, ttl: ttl, failureTTL: failureTTL, now: now,
		client: client, directory: directory,
	}
}

func (s *dynamicConsoleModelSource) endpointURL() string {
	if s.endpoint != "" {
		return s.endpoint
	}
	return reverseruntime.GlobalEndpointTable().Resolve("console_list_models")
}

func (s *dynamicConsoleModelSource) List() []model.ModelSpec {
	return s.ListContext(context.Background())
}

func (s *dynamicConsoleModelSource) ListContext(ctx context.Context) []model.ModelSpec {
	s.mu.Lock()
	now := s.now()
	if now.Before(s.expiresAt) {
		s.cacheHits++
		defer s.mu.Unlock()
		return cloneModelSpecs(s.cache)
	}
	s.cacheMisses++

	if len(s.cache) > 0 {
		cached := cloneModelSpecs(s.cache)
		s.startRefreshLocked(context.WithoutCancel(ctx), now)
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	specs, err := s.fetch(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.cache = cloneModelSpecs(specs)
		s.expiresAt = now.Add(s.ttl)
		s.lastErrAt = time.Time{}
		s.lastErr = ""
		s.refreshSuccesses++
		s.lastSuccessAt = now
		return cloneModelSpecs(s.cache)
	}
	s.lastErrAt = now
	s.lastErr = err.Error()
	s.refreshFailures++
	s.lastFailureAt = now
	s.expiresAt = now.Add(s.failureTTL)
	return cloneModelSpecs(s.cache)
}

func (s *dynamicConsoleModelSource) LastErrorTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErrAt
}

func (s *dynamicConsoleModelSource) Status() DynamicConsoleModelStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return DynamicConsoleModelStatus{
		CachedModels:     len(s.cache),
		CacheHits:        s.cacheHits,
		CacheMisses:      s.cacheMisses,
		RefreshSuccesses: s.refreshSuccesses,
		RefreshFailures:  s.refreshFailures,
		Refreshing:       s.refreshing,
		ExpiresAt:        s.expiresAt,
		LastSuccessAt:    s.lastSuccessAt,
		LastFailureAt:    s.lastFailureAt,
		LastError:        s.lastErr,
	}
}

func DynamicModelStatus() DynamicConsoleModelStatus {
	return defaultDynamicConsoleModels.Status()
}

func (s *dynamicConsoleModelSource) startRefreshLocked(ctx context.Context, now time.Time) {
	if s.refreshing {
		return
	}
	s.refreshing = true
	go func() {
		specs, err := s.fetch(ctx)
		finished := s.now()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.refreshing = false
		if err != nil {
			s.lastErrAt = finished
			s.lastErr = err.Error()
			s.expiresAt = finished.Add(s.failureTTL)
			s.refreshFailures++
			s.lastFailureAt = finished
			return
		}
		s.cache = cloneModelSpecs(specs)
		s.expiresAt = finished.Add(s.ttl)
		s.lastErrAt = time.Time{}
		s.lastErr = ""
		s.refreshSuccesses++
		s.lastSuccessAt = finished
	}()
	_ = now
}

func (s *dynamicConsoleModelSource) fetch(ctx context.Context) ([]model.ModelSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory := s.directory()
	if directory == nil {
		return nil, errors.New("account directory is not initialised")
	}
	account, ok, err := directory.ReserveChatAccount(ctx, model.ModelSpec{
		ModeID: model.ModeConsole, Tier: model.TierBasic, Capability: model.CapabilityConsoleChat, Enabled: true,
	}, nil)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("no console account is available")
	}
	defer func() { _ = directory.ReleaseChatAccount(ctx, account) }()

	return s.listModels(ctx, account.Token)
}

func (s *dynamicConsoleModelSource) ProbeListModels(ctx context.Context, token string) error {
	_, err := s.listModels(ctx, token)
	return err
}

func ProbeConsoleListModels(ctx context.Context, token string) error {
	return defaultDynamicConsoleModels.ProbeListModels(ctx, token)
}

func parseDynamicConsoleModelSpecs(raw []byte, headers map[string]string) ([]model.ModelSpec, error) {
	parsed, err := transport.ParseGRPCWebResponse(raw, headers["content-type"], headers)
	if err != nil {
		return nil, err
	}
	return parseDynamicConsoleTransportSpecs(parsed)
}

func parseDynamicConsoleTransportSpecs(parsed transport.GRPCWebTransportResponse) ([]model.ModelSpec, error) {
	if status := strings.TrimSpace(parsed.Trailers["grpc-status"]); status != "" && status != "0" {
		return nil, fmt.Errorf("list models grpc status=%s message=%q", status, parsed.Trailers["grpc-message"])
	}
	return specsFromDynamicConsoleMessages(parsed.Messages), nil
}

func (s *dynamicConsoleModelSource) listModels(ctx context.Context, token string) ([]model.ModelSpec, error) {
	if s.client != nil {
		raw, headers, err := s.postListModels(ctx, token)
		if err != nil {
			return nil, err
		}
		return parseDynamicConsoleModelSpecs(raw, headers)
	}
	parsed, err := s.postListModelsTransport(ctx, token)
	if err != nil {
		return nil, err
	}
	return parseDynamicConsoleTransportSpecs(parsed)
}

func (s *dynamicConsoleModelSource) postListModelsTransport(ctx context.Context, token string) (transport.GRPCWebTransportResponse, error) {
	proxyRuntime, err := defaultProxyTransportRuntime(ctx)
	if err != nil {
		return transport.GRPCWebTransportResponse{}, err
	}
	lease, err := proxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope:           controlproxy.ProxyScopeApp,
		Kind:            controlproxy.RequestKindHTTP,
		ClearanceOrigin: reverseruntime.GlobalEndpointTable().Resolve("console_base"),
	})
	if err != nil {
		return transport.GRPCWebTransportResponse{}, err
	}
	feedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackSuccess)
	defer func() { _ = proxyRuntime.Feedback(ctx, lease, feedback) }()

	parsed, err := transport.PostGRPCWeb(ctx, protocol.GRPCWebRequest{
		URL:      s.endpointURL(),
		Token:    token,
		Payload:  transport.EncodeGRPCWebPayload(nil),
		Lease:    &lease,
		TimeoutS: 20,
		Origin:   reverseruntime.GlobalEndpointTable().Resolve("console_base"),
		Referer:  reverseruntime.GlobalEndpointTable().Resolve("console_referer"),
	})
	if err != nil {
		feedback = chatProxyFeedbackForError(err)
		return transport.GRPCWebTransportResponse{}, err
	}
	if status := strings.TrimSpace(parsed.Trailers["grpc-status"]); status != "" && status != "0" {
		feedback = controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackUpstream5xx)
	}
	return parsed, nil
}

func (s *dynamicConsoleModelSource) postListModels(ctx context.Context, token string) ([]byte, map[string]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpointURL(), bytes.NewReader(transport.EncodeGRPCWebPayload(nil)))
	if err != nil {
		return nil, nil, err
	}
	headers := proxyadapters.BuildConsoleHeaders(token, proxyadapters.ConsoleHeaderOptions{ContentType: "application/grpc-web+proto"})
	delete(headers, "Accept-Encoding")
	headers["Accept"] = "application/grpc-web+proto"
	headers["Content-Type"] = "application/grpc-web+proto"
	headers["x-grpc-web"] = "1"
	headers["x-user-agent"] = "connect-es/2.1.1"
	headers["Cache-Control"] = "no-cache"
	headers["Pragma"] = "no-cache"
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	raw, err := readDynamicConsoleResponseBody(response.Body)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("list models returned status %d", response.StatusCode)
	}
	return raw, lowerHTTPHeaders(response.Header), nil
}

func readDynamicConsoleResponseBody(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxDynamicConsoleListModelsResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxDynamicConsoleListModelsResponseBytes {
		return nil, fmt.Errorf("list models response body exceeds %d bytes", maxDynamicConsoleListModelsResponseBytes)
	}
	return raw, nil
}

func specsFromDynamicConsoleMessages(messages [][]byte) []model.ModelSpec {
	names := map[string]struct{}{}
	for _, message := range messages {
		fields, ok := parseDynamicProtoFields(message, 0)
		if !ok {
			continue
		}
		collectDynamicConsoleNames(fields, names)
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		if isDynamicConsoleChatModel(name) {
			ordered = append(ordered, name)
		}
	}
	slices.Sort(ordered)

	specs := make([]model.ModelSpec, 0, len(ordered))
	for _, name := range ordered {
		specs = append(specs, model.ModelSpec{
			ModelName: name, ModeID: model.ModeConsole, Tier: model.TierBasic,
			Capability: model.CapabilityConsoleChat, Enabled: true,
			PublicName: dynamicConsolePublicName(name),
		})
	}
	return specs
}

type dynamicProtoField struct {
	Number   int
	String   string
	Children []dynamicProtoField
}

func parseDynamicProtoFields(data []byte, depth int) ([]dynamicProtoField, bool) {
	if depth > 6 {
		return nil, false
	}
	fields := []dynamicProtoField{}
	for offset := 0; offset < len(data); {
		tag, next, ok := readDynamicVarint(data, offset)
		if !ok {
			return fields, false
		}
		offset = next
		number := int(tag >> 3)
		wire := int(tag & 0x07)
		if number <= 0 || number > 1000 {
			return fields, false
		}
		field := dynamicProtoField{Number: number}
		switch wire {
		case 0:
			_, next, ok := readDynamicVarint(data, offset)
			if !ok {
				return fields, false
			}
			offset = next
		case 1:
			if offset+8 > len(data) {
				return fields, false
			}
			offset += 8
		case 2:
			length, next, ok := readDynamicVarint(data, offset)
			if !ok || length > uint64(len(data)-next) {
				return fields, false
			}
			offset = next
			value := data[offset : offset+int(length)]
			offset += int(length)
			if dynamicProtoText(value) {
				field.String = string(value)
			} else if children, ok := parseDynamicProtoFields(value, depth+1); ok && len(children) > 0 {
				field.Children = children
			}
		case 5:
			if offset+4 > len(data) {
				return fields, false
			}
			offset += 4
		default:
			return fields, false
		}
		fields = append(fields, field)
	}
	return fields, true
}

func readDynamicVarint(data []byte, offset int) (uint64, int, bool) {
	var value uint64
	for shift := uint(0); offset < len(data) && shift <= 63; shift += 7 {
		b := data[offset]
		offset++
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, offset, true
		}
	}
	return 0, offset, false
}

func dynamicProtoText(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	text := string(data)
	printable := 0
	total := 0
	for _, r := range text {
		total++
		if r == '\n' || r == '\r' || r == '\t' || (unicode.IsPrint(r) && r != unicode.ReplacementChar) {
			printable++
		}
	}
	return total > 0 && printable == total
}

func collectDynamicConsoleNames(fields []dynamicProtoField, names map[string]struct{}) {
	hasPrimary := false
	aliases := []string{}
	for _, field := range fields {
		if field.Number == 1 && isGrokModelID(field.String) {
			names[field.String] = struct{}{}
			hasPrimary = true
		}
		if field.Number == 14 && isGrokModelID(field.String) {
			aliases = append(aliases, field.String)
		}
	}
	if hasPrimary {
		for _, alias := range aliases {
			names[alias] = struct{}{}
		}
	}
	for _, field := range fields {
		if len(field.Children) > 0 {
			collectDynamicConsoleNames(field.Children, names)
		}
	}
}

func isGrokModelID(value string) bool {
	return strings.HasPrefix(strings.ToLower(value), "grok-")
}

func isDynamicConsoleChatModel(value string) bool {
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "grok-") {
		return false
	}
	return !strings.Contains(lower, "image") && !strings.Contains(lower, "video")
}

func dynamicConsolePublicName(modelID string) string {
	parts := strings.FieldsFunc(modelID, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func lowerHTTPHeaders(headers http.Header) map[string]string {
	result := map[string]string{}
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		result[strings.ToLower(key)] = values[0]
	}
	return result
}

func cloneModelSpecs(specs []model.ModelSpec) []model.ModelSpec {
	if len(specs) == 0 {
		return nil
	}
	return slices.Clone(specs)
}
