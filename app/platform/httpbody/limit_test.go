package httpbody

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultBodyLimitsMatchRouteContracts(t *testing.T) {
	if DefaultJSONLimitBytes != 4<<20 {
		t.Fatalf("DefaultJSONLimitBytes = %d", DefaultJSONLimitBytes)
	}
	if DefaultMultipartLimitBytes != 64<<20 {
		t.Fatalf("DefaultMultipartLimitBytes = %d", DefaultMultipartLimitBytes)
	}
}

func TestLimitNoopForNonPositiveLimit(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("body"))
	body := req.Body
	Limit(httptest.NewRecorder(), req, 0)
	if req.Body != body {
		t.Fatalf("Limit changed body for non-positive limit")
	}
}

func TestLimitUsesMaxBytesReader(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("abcd"))
	Limit(httptest.NewRecorder(), req, 3)
	if _, err := io.ReadAll(req.Body); err == nil {
		t.Fatalf("ReadAll over limit error = nil")
	}
}
