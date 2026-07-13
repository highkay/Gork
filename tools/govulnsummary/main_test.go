package main

import (
	"strings"
	"testing"
)

func TestSummarize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    vulnSummary
		wantErr bool
	}{
		{
			name: "empty input",
		},
		{
			name: "non finding events",
			input: `{"config":{"scanner_name":"govulncheck"}}
{"progress":{"message":"scan"}}
`,
		},
		{
			name: "called and import only findings",
			input: `{"finding":{"osv":"GO-1","trace":[{"module":"example.com/app"}]}}
{"finding":{"osv":"GO-2"}}
`,
			want: vulnSummary{Findings: 2, CalledFindings: 1, ImportFindings: 1},
		},
		{
			name:    "invalid json",
			input:   `{"finding":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := summarize(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("summarize returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("summarize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
