package openai

import "testing"

func TestParseConsole429Info(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantPS   bool
		wantPM   bool
	}{
		{
			name: "per-minute only",
			body: `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Minute (actual/limit): 1922/60."}`,
			wantPS: false,
			wantPM: true,
		},
		{
			name: "per-second only",
			body: `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Second (actual/limit): 2/2."}`,
			wantPS: true,
			wantPM: false,
		},
		{
			name: "both",
			body: `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Second (actual/limit): 2/2, Requests per Minute (actual/limit): 1922/60."}`,
			wantPS: true,
			wantPM: true,
		},
		{
			name: "per-second hit, per-minute ok",
			body: `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Second (actual/limit): 2/2, Requests per Minute (actual/limit): 0/60."}`,
			wantPS: true,
			wantPM: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parseConsole429Info(tt.body)
			if info.IsPerSecondHit != tt.wantPS {
				t.Errorf("IsPerSecondHit = %v, want %v", info.IsPerSecondHit, tt.wantPS)
			}
			if info.IsPerMinuteHit != tt.wantPM {
				t.Errorf("IsPerMinuteHit = %v, want %v", info.IsPerMinuteHit, tt.wantPM)
			}
		})
	}
}
