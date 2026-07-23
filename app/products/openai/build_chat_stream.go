package openai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
)

// sessionSeedFromMessages 用末条用户文本作 prompt cache 会话种子（无显式键时）。
func sessionSeedFromMessages(msgs []build.ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "user") {
			if s := strings.TrimSpace(msgs[i].Content); s != "" {
				if len(s) > 256 {
					return s[:256]
				}
				return s
			}
		}
	}
	return ""
}

// runBuildCompletion 在已选定账号列表上尝试推理（非流/流式共用选号循环）。
func runBuildCompletion(
	ctx context.Context,
	options chatCompletionOptions,
	upstream string,
	stream bool,
	accounts []buildaccount.Account,
	dir buildAccountDirectory,
	client buildHTTPClient,
	oauth buildTokenRefresher,
) (chatCompletionResult, error) {
	msgs := build.ExtractChatMessages(options.Messages)
	// 亲和优先级：显式 override > Header/body 会话种子 > 末条用户文本回退。
	sessionSeed := strings.TrimSpace(options.PromptCacheSeed)
	if sessionSeed == "" {
		sessionSeed = sessionSeedFromMessages(msgs)
	}
	cacheKey := build.ResolvePromptCacheKey(
		build.PromptCacheKeyFromOverrides(options.RequestOverrides),
		sessionSeed,
		upstream,
	)
	body, err := build.BuildResponsesBodyOpts(build.ResponsesBodyOptions{
		Model:          upstream,
		Messages:       msgs,
		Stream:         stream,
		Tools:          options.Tools,
		ToolChoice:     options.ToolChoice,
		PromptCacheKey: cacheKey,
		ResponseFormat: options.ResponseFormat,
	})
	if err != nil {
		return chatCompletionResult{}, platform.NewUpstreamError(err.Error(), 400, "")
	}

	// 多轮推理回放：将上一轮 opaque reasoning 注入 input（session = prompt_cache_key）。
	if cacheKey != "" {
		if replay := build.DefaultReasoningReplay(); replay != nil {
			body = replay.Apply(ctx, upstream, cacheKey, body)
		}
	}

	// Cache-capable 路由注入（tool-free → web_search+x_search + tool_choice=none）。
	allowClientTools := build.AllowClientToolCacheRoute(sessionSeed, "")
	routedBody, cacheRoute, routeErr := build.PreparePromptCacheRoute(body, "chat", upstream, cacheKey, allowClientTools)
	if routeErr != nil {
		return chatCompletionResult{}, platform.NewUpstreamError(routeErr.Error(), 400, "")
	}
	body = routedBody

	meta := build.RequestMeta{
		Model:          upstream,
		Stream:         stream,
		PromptCacheKey: cacheKey,
		TurnIndex:      options.GrokTurnIndex,
	}
	var lastErr error
	for _, acc := range accounts {
		access, err := ensureBuildAccessToken(ctx, dir, oauth, acc)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := invokeBuildOnce(ctx, options.Model, meta, body, cacheRoute, acc, access, dir, client, oauth)
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

// buildRecoveringClient 可选：支持 reasoning decode 恢复的 Build 客户端。
type buildRecoveringClient interface {
	CreateResponseRecovering(ctx context.Context, meta build.RequestMeta, body []byte) (*http.Response, build.ReasoningRecoveryOutcome, error)
}

func invokeBuildOnce(
	ctx context.Context,
	modelName string,
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
			return readBuildResponse(modelName, meta.Model, meta.PromptCacheKey, meta.Stream, resp2, cacheRoute, outcome)
		} else if build.IsPermanentRefresh(rerr) {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return chatCompletionResult{}, handleBuildUpstreamFailure(ctx, dir, acc, resp.StatusCode, string(raw), "create_response")
	}
	return readBuildResponse(modelName, meta.Model, meta.PromptCacheKey, meta.Stream, resp, cacheRoute, outcome)
}

func createBuildResponse(
	ctx context.Context,
	client buildHTTPClient,
	meta build.RequestMeta,
	body []byte,
) (*http.Response, build.ReasoningRecoveryOutcome, error) {
	if recovering, ok := client.(buildRecoveringClient); ok {
		return recovering.CreateResponseRecovering(ctx, meta, body)
	}
	// *build.APIClient 实现 CreateResponseRecovering；默认客户端走恢复路径。
	if api, ok := client.(*build.APIClient); ok {
		return api.CreateResponseRecovering(ctx, meta, body)
	}
	resp, err := client.CreateResponse(ctx, meta, bytes.NewReader(body))
	return resp, build.ReasoningRecoveryOutcome{}, err
}

func readBuildResponse(
	modelName, upstreamModel, replayKey string,
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
	// 捕获上游响应写入推理回放缓存（流式：包装 Body；非流：读完整 JSON）。
	if replayKey != "" {
		if replay := build.DefaultReasoningReplay(); replay != nil && replay.Enabled() {
			resp.Body = replay.CaptureBody(resp.Body, upstreamModel, replayKey, stream, false)
		}
	}
	if stream {
		frames, err := build.ChatStreamFramesFromResponsesSSE(modelName, chatResponseID(), resp.Body)
		if err != nil {
			return chatCompletionResult{}, err
		}
		// CaptureBody 在 Close 时落库；确保消费完关闭。
		_ = resp.Body.Close()
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
	if err != nil {
		return chatCompletionResult{}, err
	}
	return finishBuildChat(modelName, raw)
}
