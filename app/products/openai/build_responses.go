package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
)

// BuildResponses 原生 /v1/responses → Build 上游 /responses（不经 chat.completion 再包装）。
// 非流：返回 object=response JSON；流：输出 response.* SSE 帧。
func BuildResponses(ctx context.Context, options responseOptions) (chatCompletionResult, error) {
	if !buildFeatureEnabled() {
		return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
	}
	upstream := model.UpstreamIDFromBuildModel(options.Model)
	if upstream == "" {
		return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
	}
	dir := buildAccountDir()
	if dir == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Build account directory not initialised")
	}
	accounts, err := dir.ListActive(ctx, time.Now().UTC())
	if err != nil {
		return chatCompletionResult{}, err
	}
	accounts = filterBuildAccountsByBilling(ctx, dir, accounts)
	if len(accounts) == 0 {
		return chatCompletionResult{}, platform.NewRateLimitError("No available Build accounts")
	}

	messages := responseMessages(options.Instructions, options.Input)
	chatMsgs := build.ExtractChatMessages(messages)
	sessionSeed := sessionSeedFromMessages(chatMsgs)
	if seed := strings.TrimSpace(options.PromptCacheSeed); seed != "" {
		sessionSeed = seed
	}
	cacheKey := build.ResolvePromptCacheKey(
		build.PromptCacheKeyFromOverrides(options.RequestOverrides),
		sessionSeed,
		upstream,
	)

	body, err := build.BuildResponsesBodyOpts(build.ResponsesBodyOptions{
		Model:          upstream,
		Messages:       chatMsgs,
		Stream:         options.Stream,
		Tools:          options.Tools,
		ToolChoice:     options.ToolChoice,
		PromptCacheKey: cacheKey,
	})
	if err != nil {
		return chatCompletionResult{}, platform.NewUpstreamError(err.Error(), 400, "")
	}
	if cacheKey != "" {
		if replay := build.DefaultReasoningReplay(); replay != nil {
			body = replay.Apply(ctx, upstream, cacheKey, body)
		}
	}
	allowClientTools := build.AllowClientToolCacheRoute(sessionSeed, "")
	body, cacheRoute, err := build.PreparePromptCacheRoute(body, "responses", upstream, cacheKey, allowClientTools)
	if err != nil {
		return chatCompletionResult{}, platform.NewUpstreamError(err.Error(), 400, "")
	}

	meta := build.RequestMeta{
		Model:          upstream,
		Stream:         options.Stream,
		PromptCacheKey: cacheKey,
		TurnIndex:      options.GrokTurnIndex,
	}
	var lastErr error
	for _, acc := range accounts {
		access, err := ensureBuildAccessToken(ctx, dir, buildOAuthClient(), acc)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := invokeBuildResponsesOnce(ctx, options.Model, meta, body, cacheRoute, acc, access, dir, buildAPIClient(), buildOAuthClient())
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return chatCompletionResult{}, lastErr
	}
	return chatCompletionResult{}, platform.NewRateLimitError("No available Build accounts")
}

func invokeBuildResponsesOnce(
	ctx context.Context,
	publicModel string,
	meta build.RequestMeta,
	body []byte,
	cacheRoute build.PromptCacheRoute,
	acc buildaccount.Account,
	access string,
	dir buildAccountDirectory,
	client buildHTTPClient,
	oauth buildTokenRefresher,
) (chatCompletionResult, error) {
	reqMeta := meta
	reqMeta.AccessToken = access
	reqMeta.UserID = acc.UserID
	resp, outcome, err := createBuildResponse(ctx, client, reqMeta, body)
	if err != nil {
		return chatCompletionResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && acc.RefreshToken != "" {
		if tok, rerr := oauth.Refresh(ctx, acc.RefreshToken); rerr == nil {
			_ = dir.UpdateTokens(ctx, acc.ID, tok.AccessToken, firstNonEmptyStr(tok.RefreshToken, acc.RefreshToken), tok.ExpiresAt)
			retryMeta := reqMeta
			retryMeta.AccessToken = tok.AccessToken
			resp2, outcome2, err2 := createBuildResponse(ctx, client, retryMeta, body)
			if err2 != nil {
				return chatCompletionResult{}, err2
			}
			defer resp2.Body.Close()
			outcome = outcome.Merge(outcome2)
			return readBuildResponsesResult(publicModel, meta.Model, meta.PromptCacheKey, meta.Stream, resp2, cacheRoute, outcome)
		} else if build.IsPermanentRefresh(rerr) {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return chatCompletionResult{}, handleBuildUpstreamFailure(ctx, dir, acc, resp.StatusCode, string(raw), "create_response")
	}
	return readBuildResponsesResult(publicModel, meta.Model, meta.PromptCacheKey, meta.Stream, resp, cacheRoute, outcome)
}

func readBuildResponsesResult(
	publicModel, upstreamModel, replayKey string,
	stream bool,
	resp *http.Response,
	cacheRoute build.PromptCacheRoute,
	outcome build.ReasoningRecoveryOutcome,
) (chatCompletionResult, error) {
	if resp != nil && resp.Header != nil {
		build.ApplyReasoningRecoveryWarnings(resp.Header, outcome)
	}
	if err := build.FilterPromptCacheResponse(resp, stream, cacheRoute); err != nil {
		return chatCompletionResult{}, err
	}
	if replayKey != "" {
		if replay := build.DefaultReasoningReplay(); replay != nil && replay.Enabled() {
			resp.Body = replay.CaptureBody(resp.Body, upstreamModel, replayKey, stream, false)
		}
	}
	if stream {
		frames, err := build.ResponsesStreamFramesFromSSE(publicModel, MakeRespID("resp"), resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return chatCompletionResult{}, err
		}
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
	if err != nil {
		return chatCompletionResult{}, err
	}
	payload, err := build.NormalizeResponsesJSON(publicModel, MakeRespID("resp"), raw)
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{Response: payload}, nil
}
