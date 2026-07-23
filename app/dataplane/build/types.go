package build

import "time"

// 与 chenyme cli provider 对齐的 OAuth / 凭据默认值。
const (
	DefaultOAuthClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	DefaultOAuthScope    = "openid profile email offline_access grok-cli:access api:access"
	DefaultDeviceURL     = "https://auth.x.ai/oauth2/device/code"
	DefaultTokenURL      = "https://auth.x.ai/oauth2/token"
	DefaultBaseURL       = "https://cli-chat-proxy.grok.com/v1"
	// 与 chenyme/grok2api v3.0.7（Build 0.2.110）协议对齐。
	DefaultClientVersion = "0.2.110"
	DefaultClientIDName  = "grok-shell"
	DefaultTokenAuth     = "xai-grok-cli"
	DefaultUserAgent     = "grok-shell/" + DefaultClientVersion + " (linux; x86_64)"
	CredentialProvider   = "grok_build"

	// DefaultResponseHeaderTimeout 等待上游响应头的默认超时（对齐 chenyme #750）。
	DefaultResponseHeaderTimeout = 5 * time.Minute
	MinResponseHeaderTimeout     = 30 * time.Second
	MaxResponseHeaderTimeout     = 30 * time.Minute
)

// OAuthConfig 控制 Device OAuth 端点与 client 身份。
type OAuthConfig struct {
	ClientID  string
	Scope     string
	DeviceURL string
	TokenURL  string
}

// Normalize 填入默认值。
func (c OAuthConfig) Normalize() OAuthConfig {
	if c.ClientID == "" {
		c.ClientID = DefaultOAuthClientID
	}
	if c.Scope == "" {
		c.Scope = DefaultOAuthScope
	}
	if c.DeviceURL == "" {
		c.DeviceURL = DefaultDeviceURL
	}
	if c.TokenURL == "" {
		c.TokenURL = DefaultTokenURL
	}
	return c
}

// DeviceAuthorization 是 device/code 启动结果。
type DeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                time.Duration
	ExpiresIn               time.Duration
}

// TokenPayload 是 token 端点成功响应的规范化结果。
type TokenPayload struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	IDToken      string
}

// Credential 是可落库 / 可导入的 Build 账号凭据（明文态）。
type Credential struct {
	Provider     string
	Name         string
	ClientID     string
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    time.Time
	Email        string
	UserID       string
	TeamID       string
}

// ClientConfig 是 Build 上游 HTTP 客户端配置（B-b 将使用；B-a 仅定义）。
type ClientConfig struct {
	BaseURL               string
	ClientVersion         string
	ClientIdentifier      string
	TokenAuth             string
	UserAgent             string
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
}

// Normalize 填入默认值。
func (c ClientConfig) Normalize() ClientConfig {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.ClientVersion == "" {
		c.ClientVersion = DefaultClientVersion
	}
	if c.ClientIdentifier == "" {
		c.ClientIdentifier = DefaultClientIDName
	}
	if c.TokenAuth == "" {
		c.TokenAuth = DefaultTokenAuth
	}
	if c.UserAgent == "" {
		c.UserAgent = DefaultUserAgent
	}
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
	}
	if c.ResponseHeaderTimeout <= 0 {
		c.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	if c.ResponseHeaderTimeout < MinResponseHeaderTimeout {
		c.ResponseHeaderTimeout = MinResponseHeaderTimeout
	}
	if c.ResponseHeaderTimeout > MaxResponseHeaderTimeout {
		c.ResponseHeaderTimeout = MaxResponseHeaderTimeout
	}
	return c
}
