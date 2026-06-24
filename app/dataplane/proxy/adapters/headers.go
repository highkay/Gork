package adapters

// Header construction is split by concern:
// options and sanitizing in headers_options.go,
// cookies in headers_cookie.go,
// HTTP/WS/console headers in headers_http.go, headers_ws.go, and headers_console.go,
// and request id/statsig helpers in headers_random.go.
