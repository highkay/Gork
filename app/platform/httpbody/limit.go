package httpbody

import "net/http"

const (
	DefaultJSONLimitBytes      int64 = 4 << 20
	DefaultMultipartLimitBytes int64 = 64 << 20
)

func Limit(w http.ResponseWriter, r *http.Request, limit int64) {
	if r == nil || r.Body == nil || limit <= 0 {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
}

func LimitJSON(w http.ResponseWriter, r *http.Request) {
	Limit(w, r, DefaultJSONLimitBytes)
}

func LimitMultipart(w http.ResponseWriter, r *http.Request) {
	Limit(w, r, DefaultMultipartLimitBytes)
}
