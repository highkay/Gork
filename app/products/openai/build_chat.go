package openai

import (
	"bytes"
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
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

// 可注入依赖，便于单测；默认读 GlobalConfig + 可选 Build 账号目录。
var (
	buildFeatureEnabled = func() bool {
		return platformconfig.GlobalConfig.GetBool("features.build_provider", false)
	}
	buildCompletions = BuildCompletions
	buildAccountDir  = func() buildAccountDirectory { return defaultBuildAccountDirectory }
	buildAPIClient   = defaultBuildAPIClient
	buildOAuthClient = defaultBuildOAuthClient
)

// buildAccountDirectory 独立选号（不碰 SSO 池）。
type buildAccountDirectory interface {
	ListActive(ctx context.Context, now time.Time) ([]buildaccount.Account, error)
	UpdateTokens(ctx context.Context, id int64, access, refresh string, expiresAt time.Time) error
	SetStatus(ctx context.Context, id int64, status string, reason string) error
}

// defaultBuildAccountDirectory 由启动阶段注入；nil 表示未挂载。
var defaultBuildAccountDirectory buildAccountDirectory

// SetBuildAccountDirectory 挂载 Build 账号池（B-c 启动接线也可调用）。
func SetBuildAccountDirectory(dir buildAccountDirectory) {
	defaultBuildAccountDirectory = dir
}

type buildHTTPClient interface {
	CreateResponse(ctx context.Context, meta build.RequestMeta, body io.Reader) (*http.Response, error)
}

type buildTokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (build.TokenPayload, error)
}

func defaultBuildAPIClient() buildHTTPClient {
	cfg := build.ClientConfig{
		BaseURL:          platformconfig.GlobalConfig.GetStr("provider.build.base_url", build.DefaultBaseURL),
		ClientVersion:    platformconfig.GlobalConfig.GetStr("provider.build.client_version", build.DefaultClientVersion),
		ClientIdentifier: platformconfig.GlobalConfig.GetStr("provider.build.client_identifier", build.DefaultClientIDName),
		TokenAuth:        platformconfig.GlobalConfig.GetStr("provider.build.token_auth", build.DefaultTokenAuth),
		UserAgent:        platformconfig.GlobalConfig.GetStr("provider.build.user_agent", build.DefaultUserAgent),
		Timeout:          time.Duration(platformconfig.GlobalConfig.GetFloat("provider.build.timeout_seconds", 120)) * time.Second,
	}
	return build.NewAPIClient(nil, cfg)
}

func defaultBuildOAuthClient() buildTokenRefresher {
	cfg := build.OAuthConfig{
		ClientID:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_client_id", build.DefaultOAuthClientID),
		Scope:     platformconfig.GlobalConfig.GetStr("provider.build.oauth_scope", build.DefaultOAuthScope),
		DeviceURL: platformconfig.GlobalConfig.GetStr("provider.build.oauth_device_url", build.DefaultDeviceURL),
		TokenURL:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_token_url", build.DefaultTokenURL),
	}
	return build.NewOAuthClient(nil, cfg)
}

// BuildCompletions 走 Build 上游 POST /responses，再转 OpenAI chat.completion。
// 首批仅非流式；stream=true 返回 400 提示（避免半截 SSE）。
func BuildCompletions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
	if !buildFeatureEnabled() {
		return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
	}
	upstream := model.UpstreamIDFromBuildModel(options.Model)
	if upstream == "" {
		return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
	}
	isStream := false
	if options.Stream != nil {
		isStream = *options.Stream
	}
	if isStream {
		return chatCompletionResult{}, platform.NewUpstreamError(
			"Build chat streaming is not enabled in this release; set stream=false",
			400, "",
		)
	}

	dir := buildAccountDir()
	if dir == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Build account directory not initialised")
	}
	accounts, err := dir.ListActive(ctx, time.Now().UTC())
	if err != nil {
		return chatCompletionResult{}, err
	}
	if len(accounts) == 0 {
		return chatCompletionResult{}, platform.NewRateLimitError("No available Build accounts")
	}

	msgs := build.ExtractChatMessages(options.Messages)
	body, err := build.BuildResponsesBody(upstream, msgs, false)
	if err != nil {
		return chatCompletionResult{}, platform.NewUpstreamError(err.Error(), 400, "")
	}

	client := buildAPIClient()
	oauth := buildOAuthClient()
	var lastErr error
	for _, acc := range accounts {
		access, err := ensureBuildAccessToken(ctx, dir, oauth, acc)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.CreateResponse(ctx, build.RequestMeta{
			AccessToken: access,
			UserID:      acc.UserID,
			Model:       upstream,
			Stream:      false,
		}, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized {
			// 尝试一次 refresh 后重试本账号
			if acc.RefreshToken != "" {
				if tok, rerr := oauth.Refresh(ctx, acc.RefreshToken); rerr == nil {
					_ = dir.UpdateTokens(ctx, acc.ID, tok.AccessToken, firstNonEmptyStr(tok.RefreshToken, acc.RefreshToken), tok.ExpiresAt)
					resp2, err2 := client.CreateResponse(ctx, build.RequestMeta{
						AccessToken: tok.AccessToken,
						UserID:      acc.UserID,
						Model:       upstream,
						Stream:      false,
					}, bytes.NewReader(body))
					if err2 == nil {
						raw2, _ := io.ReadAll(io.LimitReader(resp2.Body, 8<<20))
						_ = resp2.Body.Close()
						if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
							return finishBuildChat(options.Model, raw2)
						}
						lastErr = &build.UpstreamError{Status: resp2.StatusCode, Body: string(raw2), Op: "create_response"}
						continue
					}
				} else if build.IsPermanentRefresh(rerr) {
					_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
				}
			}
			lastErr = &build.UpstreamError{Status: resp.StatusCode, Body: string(raw), Op: "create_response"}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = &build.UpstreamError{Status: resp.StatusCode, Body: string(raw), Op: "create_response"}
			if build.IsRateLimited(&build.UpstreamError{Status: resp.StatusCode}) {
				_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusCooling, fmt.Sprintf("upstream %d", resp.StatusCode))
			}
			continue
		}
		return finishBuildChat(options.Model, raw)
	}
	if lastErr != nil {
		return chatCompletionResult{}, lastErr
	}
	return chatCompletionResult{}, platform.NewRateLimitError("No available Build accounts")
}

func finishBuildChat(modelName string, raw []byte) (chatCompletionResult, error) {
	response, err := build.ChatCompletionFromResponsesJSON(modelName, chatResponseID(), raw)
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{Response: response}, nil
}

func ensureBuildAccessToken(
	ctx context.Context,
	dir buildAccountDirectory,
	oauth buildTokenRefresher,
	acc buildaccount.Account,
) (string, error) {
	if acc.AccessToken != "" && !acc.NeedsRefresh(time.Now().UTC(), 2*time.Minute) {
		return acc.AccessToken, nil
	}
	if acc.RefreshToken == "" {
		if acc.AccessToken != "" {
			return acc.AccessToken, nil
		}
		return "", fmt.Errorf("build account %d has no tokens", acc.ID)
	}
	tok, err := oauth.Refresh(ctx, acc.RefreshToken)
	if err != nil {
		if build.IsPermanentRefresh(err) {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
		}
		return "", err
	}
	refresh := firstNonEmptyStr(tok.RefreshToken, acc.RefreshToken)
	_ = dir.UpdateTokens(ctx, acc.ID, tok.AccessToken, refresh, tok.ExpiresAt)
	return tok.AccessToken, nil
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
