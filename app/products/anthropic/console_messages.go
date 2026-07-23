package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	controlaccount "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/control/model"
	dataaccount "github.com/dslzl/gork/app/dataplane/account"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	appruntime "github.com/dslzl/gork/app/platform/runtime"
	"github.com/dslzl/gork/app/products"
)

type consoleMessagesFeedbackKind string

const (
	consoleMessagesFeedbackSuccess      consoleMessagesFeedbackKind = "success"
	consoleMessagesFeedbackUnauthorized consoleMessagesFeedbackKind = "unauthorized"
	consoleMessagesFeedbackServerError  consoleMessagesFeedbackKind = "server_error"
	consoleMessagesFeedbackRateLimited  consoleMessagesFeedbackKind = "rate_limited"
	consoleMessagesFeedbackForbidden    consoleMessagesFeedbackKind = "forbidden"
)

var errConsoleMessagesStreamNotConfigured = platform.NewUpstreamError("console messages stream is not configured", 502, "")

type ConsoleMessagesOptions struct {
	Model       string
	Messages    []map[string]any
	Stream      bool
	EmitThink   bool
	Temperature float64
	TopP        float64
	MessageID   string
}

type ConsoleMessagesResult struct {
	IsStream     bool
	StreamFrames []string
	Response     map[string]any
}

type consoleMessagesAccount struct {
	Token  string
	ModeID model.ModeID
	lease  dataaccount.AccountLease
}

type consoleMessagesFeedback struct {
	Token  string
	Kind   consoleMessagesFeedbackKind
	ModeID model.ModeID
}

type consoleMessagesDirectory interface {
	ReserveConsoleMessagesAccount(context.Context, model.ModelSpec, []string) (consoleMessagesAccount, bool, error)
	ReleaseConsoleMessagesAccount(context.Context, consoleMessagesAccount) error
	FeedbackConsoleMessagesAccount(context.Context, consoleMessagesFeedback) error
}

type consoleMessagesRefreshProvider interface {
	RefreshCallAsync(context.Context, string, int) error
	RecordFailureAsync(context.Context, string, int, error) error
}

var (
	consoleMessagesDirectoryProvider = defaultConsoleMessagesDirectoryProvider
	consoleMessagesStream            = func(ctx context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
		return products.StreamConsoleChat(ctx, token, payload, timeoutS)
	}
	consoleMessagesMaxRetries     = products.SelectionMaxRetries
	consoleMessagesTimeoutSeconds = defaultConsoleMessagesTimeoutSeconds
	consoleMessagesRetryCodes     = defaultConsoleMessagesRetryCodes
	consoleMessagesQuotaSync      = defaultConsoleMessagesQuotaSync
	consoleMessagesFailSync       = defaultConsoleMessagesFailSync
)

type consoleMessagesDataDirectory struct {
	directory *dataaccount.AccountDirectory
}

type consoleMessagesReserveDirectory struct {
	directory *dataaccount.AccountDirectory
}

func defaultConsoleMessagesDirectoryProvider() consoleMessagesDirectory {
	directory, err := dataaccount.GetAccountDirectory(context.Background(), nil)
	if err != nil || directory == nil {
		return nil
	}
	return consoleMessagesDataDirectory{directory: directory}
}

func (d consoleMessagesDataDirectory) ReserveConsoleMessagesAccount(ctx context.Context, spec model.ModelSpec, excluded []string) (consoleMessagesAccount, bool, error) {
	nowS := appruntime.NowS()
	lease, result, err := products.ReserveAccountDetailed(ctx, consoleMessagesReserveDirectory{directory: d.directory}, spec, products.ReserveAccountOptions{
		ExcludeTokens: excluded,
		NowSOverride:  &nowS,
	})
	if err != nil {
		return consoleMessagesAccount{}, false, err
	}
	if !result.OK {
		return consoleMessagesAccount{}, false, products.AccountSelectionError(result)
	}
	accountLease, ok := lease.(dataaccount.AccountLease)
	if !ok {
		return consoleMessagesAccount{}, false, fmt.Errorf("unexpected account lease type %T", lease)
	}
	return consoleMessagesAccount{Token: accountLease.Token, ModeID: result.SelectedMode, lease: accountLease}, true, nil
}

func (d consoleMessagesDataDirectory) ReleaseConsoleMessagesAccount(_ context.Context, account consoleMessagesAccount) error {
	d.directory.Release(account.lease)
	return nil
}

func (d consoleMessagesDataDirectory) FeedbackConsoleMessagesAccount(_ context.Context, feedback consoleMessagesFeedback) error {
	d.directory.Feedback(feedback.Token, controlaccount.FeedbackKind(feedback.Kind), int(feedback.ModeID), dataaccount.FeedbackOptions{NowS: intPtr(int(appruntime.NowS()))})
	return nil
}

func (d consoleMessagesReserveDirectory) Reserve(_ context.Context, query products.ReserveAccountQuery) (any, error) {
	lease, ok := d.directory.Reserve(query.PoolCandidates, int(query.ModeID), dataaccount.ReserveOptions{
		ExcludeTokens: query.ExcludeTokens,
		NowS:          int64PtrToIntPtr(query.NowSOverride),
	})
	if !ok {
		return nil, nil
	}
	return lease, nil
}

func (d consoleMessagesReserveDirectory) ReserveDetailed(_ context.Context, query products.ReserveAccountQuery) (any, products.AccountSelectionFailureReason, error) {
	lease, reason, ok := d.directory.ReserveDetailed(query.PoolCandidates, int(query.ModeID), dataaccount.ReserveOptions{
		ExcludeTokens: query.ExcludeTokens,
		NowS:          int64PtrToIntPtr(query.NowSOverride),
	})
	if !ok {
		return nil, products.AccountSelectionFailureFromData(reason), nil
	}
	return lease, products.AccountSelectionFailureNone, nil
}

func defaultConsoleMessagesRetryCodes() map[int]struct{} {
	raw := platformconfig.GlobalConfig.Get("retry.on_codes", nil)
	if raw == nil {
		raw = platformconfig.GlobalConfig.Get("retry.retry_status_codes", "429,401,503")
	}
	return parseConsoleMessagesRetryCodes(raw)
}

func defaultConsoleMessagesTimeoutSeconds() float64 {
	return platformconfig.GlobalConfig.GetFloat("chat.timeout", 120.0)
}

func parseConsoleMessagesRetryCodes(value any) map[int]struct{} {
	result := map[int]struct{}{}
	var parts []any
	switch typed := value.(type) {
	case string:
		for _, part := range strings.Split(typed, ",") {
			parts = append(parts, strings.TrimSpace(part))
		}
	case []any:
		parts = append(parts, typed...)
	case []string:
		for _, part := range typed {
			parts = append(parts, part)
		}
	case []int:
		for _, part := range typed {
			parts = append(parts, part)
		}
	default:
		return result
	}
	for _, part := range parts {
		text := strings.TrimSpace(fmt.Sprint(part))
		if text == "" {
			continue
		}
		code, err := strconv.Atoi(text)
		if err == nil {
			result[code] = struct{}{}
		}
	}
	return result
}

func defaultConsoleMessagesQuotaSync(ctx context.Context, token string, modeID int) {
	if dataaccount.CurrentStrategy() != "quota" {
		return
	}
	if service := consoleMessagesRefreshService(); service != nil {
		_ = service.RefreshCallAsync(ctx, token, modeID)
	}
}

func defaultConsoleMessagesFailSync(ctx context.Context, token string, modeID int, err error) {
	if service := consoleMessagesRefreshService(); service != nil {
		_ = service.RecordFailureAsync(ctx, token, modeID, err)
	}
}

func consoleMessagesRefreshService() consoleMessagesRefreshProvider {
	service := controlaccount.GetRefreshService()
	if service == nil {
		return nil
	}
	provider, ok := any(service).(consoleMessagesRefreshProvider)
	if !ok {
		return nil
	}
	return provider
}

func ConsoleMessages(ctx context.Context, options ConsoleMessagesOptions) (ConsoleMessagesResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return ConsoleMessagesResult{}, err
	}
	directory := consoleMessagesDirectoryProvider()
	if directory == nil {
		return ConsoleMessagesResult{}, platform.NewRateLimitError("Account directory not initialised")
	}
	excluded := []string{}
	maxRetries := consoleMessagesMaxRetries()
	retryCodes := consoleMessagesRetryCodes()
	for attempt := 0; attempt <= maxRetries; attempt++ {
		account, ok, err := directory.ReserveConsoleMessagesAccount(ctx, spec, excluded)
		if err != nil {
			return ConsoleMessagesResult{}, err
		}
		if !ok {
			return ConsoleMessagesResult{}, platform.NewRateLimitError("No available accounts for this model tier")
		}
		result, retry, err := runConsoleMessagesAttempt(ctx, options, account, directory, retryCodes)
		if err == nil {
			return result, nil
		}
		if retry && attempt < maxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return ConsoleMessagesResult{}, err
	}
	return ConsoleMessagesResult{}, platform.NewRateLimitError("No available accounts after retries")
}

func runConsoleMessagesAttempt(ctx context.Context, options ConsoleMessagesOptions, account consoleMessagesAccount, directory consoleMessagesDirectory, retryCodes map[int]struct{}) (ConsoleMessagesResult, bool, error) {
	success := false
	var failErr error
	defer func() {
		_ = directory.ReleaseConsoleMessagesAccount(ctx, account)
		kind := consoleMessagesFeedbackForError(failErr)
		if success {
			kind = consoleMessagesFeedbackSuccess
		}
		_ = directory.FeedbackConsoleMessagesAccount(ctx, consoleMessagesFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
		if success {
			consoleMessagesQuotaSync(ctx, account.Token, int(account.ModeID))
		} else {
			consoleMessagesFailSync(ctx, account.Token, int(account.ModeID), failErr)
		}
	}()
	result, err := consoleMessagesFromEvents(ctx, options, account.Token)
	if err != nil {
		failErr = err
		return ConsoleMessagesResult{}, shouldRetryConsoleMessages(err, retryCodes), err
	}
	success = true
	return result, false, nil
}

func consoleMessagesFromEvents(ctx context.Context, options ConsoleMessagesOptions, token string) (ConsoleMessagesResult, error) {
	payload := protocol.BuildConsolePayload(protocol.ConsolePayloadOptions{
		Messages: options.Messages, Model: options.Model,
		Temperature: options.Temperature, TopP: options.TopP,
		ReasoningEffort: consoleMessagesEffort(options.EmitThink),
		Stream:          boolPtr(true),
	})
	events, err := consoleMessagesStream(ctx, token, payload, consoleMessagesTimeoutSeconds())
	if err != nil {
		return ConsoleMessagesResult{}, err
	}
	adapter, deltas, err := feedConsoleMessagesEvents(events)
	if err != nil {
		return ConsoleMessagesResult{}, err
	}
	if options.Stream {
		return consoleMessagesStreamResult(options, deltas, adapter), nil
	}
	return consoleMessagesNonStreamResult(options, adapter), nil
}

func feedConsoleMessagesEvents(events []protocol.ConsoleStreamEvent) (*protocol.ConsoleStreamAdapter, []string, error) {
	adapter := protocol.NewConsoleStreamAdapter()
	deltas := []string{}
	for _, event := range events {
		tokens, err := adapter.Feed(event.EventType, event.Data)
		if err != nil {
			return nil, nil, err
		}
		deltas = append(deltas, tokens...)
	}
	return adapter, deltas, nil
}

func consoleMessagesNonStreamResult(options ConsoleMessagesOptions, adapter *protocol.ConsoleStreamAdapter) ConsoleMessagesResult {
	text := adapter.FullText()
	return ConsoleMessagesResult{Response: map[string]any{
		"id": options.MessageID, "type": "message", "role": "assistant", "model": options.Model,
		"content":       []map[string]any{{"type": "text", "text": text}},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage":         consoleMessagesUsage(adapter, options.Messages, text),
	}}
}

func consoleMessagesStreamResult(options ConsoleMessagesOptions, deltas []string, adapter *protocol.ConsoleStreamAdapter) ConsoleMessagesResult {
	frames := []string{
		anthropicSSE("message_start", map[string]any{"type": "message_start", "message": consoleMessagesStart(options)}),
		anthropicSSE("ping", map[string]any{"type": "ping"}),
		anthropicSSE("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}),
	}
	for _, delta := range deltas {
		frames = append(frames, anthropicSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": delta}}))
	}
	// message_delta 与非流式一致：带上 input_tokens（对齐 chenyme #614）
	usage := consoleMessagesUsage(adapter, options.Messages, adapter.FullText())
	frames = append(frames,
		anthropicSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}),
		anthropicSSE("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": usage}),
		anthropicSSE("message_stop", map[string]any{"type": "message_stop"}),
		"data: [DONE]\n\n",
	)
	return ConsoleMessagesResult{IsStream: true, StreamFrames: frames}
}

func consoleMessagesStart(options ConsoleMessagesOptions) map[string]any {
	return map[string]any{
		"id": options.MessageID, "type": "message", "role": "assistant", "model": options.Model,
		"content": []any{}, "stop_reason": nil,
		"usage": map[string]any{"input_tokens": platform.EstimatePromptTokens(options.Messages, platform.PromptOverhead), "output_tokens": 0},
	}
}

func consoleMessagesUsage(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any, text string) map[string]any {
	// 对齐 chenyme #751：input_tokens 为非缓存部分，cache_read_input_tokens 钳制到总 input。
	totalInput := int64(inputTokensFor(adapter, messages))
	cacheRead := int64(0)
	if adapter != nil && adapter.Usage != nil {
		cacheRead = int64(intFromAny(adapter.Usage["cache_read_input_tokens"]))
		if cacheRead == 0 {
			cacheRead = int64(intFromAny(adapter.Usage["cached_tokens"]))
		}
		if details, ok := adapter.Usage["input_tokens_details"].(map[string]any); ok {
			if v := intFromAny(details["cached_tokens"]); v > 0 {
				cacheRead = int64(v)
			}
		}
	}
	if cacheRead < 0 {
		cacheRead = 0
	}
	if totalInput < 0 {
		totalInput = 0
	}
	if cacheRead > totalInput {
		cacheRead = totalInput
	}
	return anthropicUsageFromTotals(totalInput, int64(outputTokensFor(adapter, text)), cacheRead)
}

func inputTokensFor(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any) int {
	if adapter.Usage != nil {
		if value := intFromAny(adapter.Usage["input_tokens"]); value != 0 {
			return value
		}
		// 部分上游把 total 放在 prompt_tokens
		if value := intFromAny(adapter.Usage["prompt_tokens"]); value != 0 {
			return value
		}
	}
	return platform.EstimatePromptTokens(messages, platform.PromptOverhead)
}

func outputTokensFor(adapter *protocol.ConsoleStreamAdapter, text string) int {
	if adapter.Usage != nil {
		if value := intFromAny(adapter.Usage["output_tokens"]); value != 0 {
			return value
		}
		if value := intFromAny(adapter.Usage["completion_tokens"]); value != 0 {
			return value
		}
	}
	return platform.EstimateTokens(text)
}

func consoleMessagesEffort(emitThink bool) string {
	if emitThink {
		return "low"
	}
	return "none"
}

func shouldRetryConsoleMessages(err error, retryCodes map[int]struct{}) bool {
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream == nil {
		return false
	}
	_, ok := retryCodes[upstream.Status]
	return ok
}

func consoleMessagesFeedbackForError(err error) consoleMessagesFeedbackKind {
	return consoleMessagesFeedbackKind(controlaccount.FeedbackKindForError(err))
}

func anthropicSSE(event string, data map[string]any) string {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte("{}")
	}
	return "event: " + event + "\ndata: " + string(raw) + "\n\n"
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func int64PtrToIntPtr(value *int64) *int {
	if value == nil {
		return nil
	}
	return intPtr(int(*value))
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
