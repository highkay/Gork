package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

// buildBillingFetcher 可注入，便于单测。
type buildBillingFetcher interface {
	GetBilling(ctx context.Context, accessToken string) (build.Billing, error)
}

var adminBuildBillingFetcher = func() buildBillingFetcher {
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

// handleAdminBuildAccountsBilling POST {id} 拉取上游 Billing 并落库。
func handleAdminBuildAccountsBilling(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, platform.NewValidationError("Invalid JSON body", "body", "invalid_json"))
		return
	}
	if req.ID <= 0 {
		writeAdminError(w, platform.NewValidationError("id is required", "id", ""))
		return
	}
	acc, err := store.Get(r.Context(), req.ID)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	access := strings.TrimSpace(acc.AccessToken)
	if access == "" {
		writeAdminError(w, platform.NewValidationError("account has no access_token", "access_token", ""))
		return
	}
	// 过期则尝试 refresh
	if acc.NeedsRefresh(time.Now().UTC(), 2*time.Minute) && acc.RefreshToken != "" {
		oauth := build.NewOAuthClient(nil, build.OAuthConfig{
			ClientID:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_client_id", build.DefaultOAuthClientID),
			Scope:     platformconfig.GlobalConfig.GetStr("provider.build.oauth_scope", build.DefaultOAuthScope),
			DeviceURL: platformconfig.GlobalConfig.GetStr("provider.build.oauth_device_url", build.DefaultDeviceURL),
			TokenURL:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_token_url", build.DefaultTokenURL),
		})
		if tok, rerr := oauth.Refresh(r.Context(), acc.RefreshToken); rerr == nil {
			access = tok.AccessToken
			_ = store.UpdateTokens(r.Context(), acc.ID, tok.AccessToken,
				firstNonEmptyAdmin(tok.RefreshToken, acc.RefreshToken), tok.ExpiresAt)
		}
	}
	billing, err := adminBuildBillingFetcher().GetBilling(r.Context(), access)
	if err != nil {
		// 失败不阻断主路径语义：管理面返回错误，但不当作账号永久失效
		writeAdminError(w, err)
		return
	}
	if err := store.UpdateBilling(r.Context(), acc.ID, billing); err != nil {
		writeAdminError(w, err)
		return
	}
	acc.Billing = billing
	acc.BillingSynced = billing.SyncedAt
	if acc.BillingSynced.IsZero() {
		acc.BillingSynced = time.Now().UTC()
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"id":      acc.ID,
		"billing": billing,
		"account": serializeBuildAccount(acc),
	})
}

func firstNonEmptyAdmin(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// silence unused import guard if buildaccount helpers evolve
var _ = buildaccount.StatusActive
