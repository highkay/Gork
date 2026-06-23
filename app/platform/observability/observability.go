package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	requestIDHeader = "X-Request-ID"
	upstreamRingMax = 10
)

type contextKey string

const requestIDContextKey contextKey = "request_id"

type UpstreamError struct {
	Time       time.Time `json:"time"`
	Product    string    `json:"product"`
	Model      string    `json:"model,omitempty"`
	StatusCode int       `json:"status_code"`
	Message    string    `json:"message"`
}

type requestMetricKey struct {
	method string
	path   string
	status int
}

type upstreamMetricKey struct {
	product string
	status  int
}

var (
	mu                  sync.Mutex
	requestCounts       = map[requestMetricKey]int{}
	requestDurationSecs = map[requestMetricKey]float64{}
	upstreamCounts      = map[upstreamMetricKey]int{}
	upstreamErrors      []UpstreamError
	upstreamErrorTotal  int
	accessLogWriter     io.Writer
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		r = r.WithContext(ctx)
		w.Header().Set(requestIDHeader, requestID)

		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		duration := time.Since(started)
		path := sanitizedPath(r)
		recordRequest(r.Method, path, recorder.status, duration)
		writeAccessLog(accessLogEntry{
			RequestID:   requestID,
			Method:      r.Method,
			Path:        path,
			Status:      recorder.status,
			DurationMS:  duration.Milliseconds(),
			Streaming:   false,
			Model:       "",
			AccountPool: "",
			ProxyScope:  "",
			UserAgent:   r.UserAgent(),
			RemoteAddr:  r.RemoteAddr,
		})
	})
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey).(string)
	return value
}

func MetricsText() string {
	mu.Lock()
	defer mu.Unlock()

	var b strings.Builder
	b.WriteString("# HELP gork_http_requests_total HTTP requests by method, path and status.\n")
	b.WriteString("# TYPE gork_http_requests_total counter\n")
	for _, key := range sortedRequestCountKeys(requestCounts) {
		fmt.Fprintf(&b, "gork_http_requests_total{method=%q,path=%q,status=%q} %d\n",
			labelValue(key.method), labelValue(key.path), strconv.Itoa(key.status), requestCounts[key])
	}
	b.WriteString("# HELP gork_http_request_duration_seconds_sum Total HTTP request duration.\n")
	b.WriteString("# TYPE gork_http_request_duration_seconds_sum counter\n")
	for _, key := range sortedRequestDurationKeys(requestDurationSecs) {
		fmt.Fprintf(&b, "gork_http_request_duration_seconds_sum{method=%q,path=%q,status=%q} %.6f\n",
			labelValue(key.method), labelValue(key.path), strconv.Itoa(key.status), requestDurationSecs[key])
	}
	b.WriteString("# HELP gork_http_errors_total HTTP error responses by method, path and status.\n")
	b.WriteString("# TYPE gork_http_errors_total counter\n")
	for _, key := range sortedRequestCountKeys(requestCounts) {
		if key.status < 400 {
			continue
		}
		fmt.Fprintf(&b, "gork_http_errors_total{method=%q,path=%q,status=%q} %d\n",
			labelValue(key.method), labelValue(key.path), strconv.Itoa(key.status), requestCounts[key])
	}
	b.WriteString("# HELP gork_upstream_errors_total Upstream errors by product and status.\n")
	b.WriteString("# TYPE gork_upstream_errors_total counter\n")
	for _, key := range sortedUpstreamKeys(upstreamCounts) {
		fmt.Fprintf(&b, "gork_upstream_errors_total{product=%q,status=%q} %d\n",
			labelValue(key.product), strconv.Itoa(key.status), upstreamCounts[key])
	}
	return b.String()
}

func Snapshot() map[string]any {
	mu.Lock()
	defer mu.Unlock()

	errors := make([]UpstreamError, len(upstreamErrors))
	copy(errors, upstreamErrors)
	totalRequests := 0
	for _, count := range requestCounts {
		totalRequests += count
	}
	return map[string]any{
		"http_requests_total":  totalRequests,
		"upstream_error_count": upstreamErrorTotal,
		"upstream_errors":      errors,
	}
}

func RecordUpstreamError(entry UpstreamError) {
	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}
	if entry.Product == "" {
		entry.Product = "unknown"
	}
	mu.Lock()
	defer mu.Unlock()

	upstreamErrorTotal++
	upstreamCounts[upstreamMetricKey{product: entry.Product, status: entry.StatusCode}]++
	upstreamErrors = append(upstreamErrors, entry)
	if len(upstreamErrors) > upstreamRingMax {
		upstreamErrors = upstreamErrors[len(upstreamErrors)-upstreamRingMax:]
	}
}

func RecentUpstreamErrors(limit int) []UpstreamError {
	mu.Lock()
	defer mu.Unlock()

	if limit <= 0 || limit > len(upstreamErrors) {
		limit = len(upstreamErrors)
	}
	start := len(upstreamErrors) - limit
	out := make([]UpstreamError, limit)
	copy(out, upstreamErrors[start:])
	return out
}

func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	requestCounts = map[requestMetricKey]int{}
	requestDurationSecs = map[requestMetricKey]float64{}
	upstreamCounts = map[upstreamMetricKey]int{}
	upstreamErrors = nil
	upstreamErrorTotal = 0
	accessLogWriter = nil
}

func SetAccessLogWriterForTest(writer io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	accessLogWriter = writer
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type accessLogEntry struct {
	RequestID   string `json:"request_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	DurationMS  int64  `json:"duration_ms"`
	Streaming   bool   `json:"streaming"`
	Model       string `json:"model,omitempty"`
	AccountPool string `json:"account_pool,omitempty"`
	ProxyScope  string `json:"proxy_scope,omitempty"`
	UserAgent   string `json:"user_agent,omitempty"`
	RemoteAddr  string `json:"remote_addr,omitempty"`
}

func recordRequest(method, path string, status int, duration time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	key := requestMetricKey{method: method, path: path, status: status}
	requestCounts[key]++
	requestDurationSecs[key] += duration.Seconds()
}

func writeAccessLog(entry accessLogEntry) {
	mu.Lock()
	writer := accessLogWriter
	mu.Unlock()
	if writer != nil {
		_ = json.NewEncoder(writer).Encode(entry)
		return
	}
	slog.Default().Info("http access",
		slog.String("request_id", entry.RequestID),
		slog.String("method", entry.Method),
		slog.String("path", entry.Path),
		slog.Int("status", entry.Status),
		slog.Int64("duration_ms", entry.DurationMS),
		slog.Bool("streaming", entry.Streaming),
		slog.String("model", entry.Model),
		slog.String("account_pool", entry.AccountPool),
		slog.String("proxy_scope", entry.ProxyScope),
	)
}

func sanitizedPath(r *http.Request) string {
	if r == nil || r.URL == nil || r.URL.Path == "" {
		return "/"
	}
	return r.URL.Path
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func labelValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\n", " ")
}

func sortedRequestCountKeys(items map[requestMetricKey]int) []requestMetricKey {
	keys := make([]requestMetricKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sortRequestKeys(keys)
	return keys
}

func sortedRequestDurationKeys(items map[requestMetricKey]float64) []requestMetricKey {
	keys := make([]requestMetricKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sortRequestKeys(keys)
	return keys
}

func sortRequestKeys(keys []requestMetricKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		return keys[i].status < keys[j].status
	})
}

func sortedUpstreamKeys(items map[upstreamMetricKey]int) []upstreamMetricKey {
	keys := make([]upstreamMetricKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].product != keys[j].product {
			return keys[i].product < keys[j].product
		}
		return keys[i].status < keys[j].status
	})
	return keys
}
