package reverse

import "testing"

func TestClassifyResultMatchesPythonStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   ResultCategory
	}{
		{name: "success", status: 200, want: ResultCategorySuccess},
		{name: "rate limited", status: 429, want: ResultCategoryRateLimited},
		{name: "unauthorized", status: 401, want: ResultCategoryAuthFailure},
		{name: "bad request invalid credentials", status: 400, body: "invalid-credentials", want: ResultCategoryAuthFailure},
		{name: "forbidden invalid credentials", status: 403, body: "token expired", want: ResultCategoryAuthFailure},
		{name: "forbidden cloudflare", status: 403, body: "<html>Cloudflare cf-challenge</html>", want: ResultCategoryForbidden},
		{name: "generic forbidden", status: 403, body: "policy denied", want: ResultCategoryForbidden},
		{name: "not found", status: 404, want: ResultCategoryNotFound},
		{name: "upstream 5xx", status: 502, want: ResultCategoryUpstream5xx},
		{name: "unknown client error", status: 418, want: ResultCategoryUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyResult(tt.status, tt.body); got != tt.want {
				t.Fatalf("ClassifyResult(%d, %q) = %v, want %v", tt.status, tt.body, got, tt.want)
			}
		})
	}
}

func TestClassifyResultHandlesBodyEdges(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   ResultCategory
	}{
		{name: "invalid credentials JSON", status: 400, body: `{"error":{"code":"INVALID-CREDENTIALS"}}`, want: ResultCategoryAuthFailure},
		{name: "uppercase token expired", status: 403, body: "TOKEN EXPIRED", want: ResultCategoryAuthFailure},
		{name: "empty forbidden body", status: 403, body: "", want: ResultCategoryForbidden},
		{name: "empty bad request body", status: 400, body: "", want: ResultCategoryUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyResult(tt.status, tt.body); got != tt.want {
				t.Fatalf("ClassifyResult(%d, %q) = %v, want %v", tt.status, tt.body, got, tt.want)
			}
		})
	}
}
