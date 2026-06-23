package transport

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const defaultMaxHTTPBodyBytes int64 = 8 << 20

var defaultNetHTTPDoer HTTPDoer = http.DefaultClient

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type netHTTPClient struct {
	Doer         HTTPDoer
	MaxBodyBytes int64
}

func (c netHTTPClient) Post(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	return c.do(ctx, http.MethodPost, request)
}

func (c netHTTPClient) Get(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	return c.do(ctx, http.MethodGet, request)
}

func (c netHTTPClient) Delete(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	return c.do(ctx, http.MethodDelete, request)
}

func doHTTPRequest(ctx context.Context, method string, request HTTPRequest) (HTTPResponse, error) {
	client := netHTTPClient{}
	return client.do(ctx, method, request)
}

func (c netHTTPClient) do(ctx context.Context, method string, request HTTPRequest) (HTTPResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	rawRequest, err := http.NewRequestWithContext(ctx, method, httpRequestURL(request), bytes.NewReader(request.Payload))
	if err != nil {
		cancel()
		return HTTPResponse{}, err
	}
	for key, value := range request.Headers {
		rawRequest.Header.Set(key, value)
	}
	if rawRequest.Header.Get("Accept-Encoding") == "" {
		rawRequest.Header.Set("Accept-Encoding", "gzip, deflate")
	}
	response, err := c.httpDoer().Do(rawRequest)
	if err != nil {
		cancel()
		return HTTPResponse{}, err
	}
	if request.Stream && response.StatusCode == 200 {
		return HTTPResponse{
			StatusCode: response.StatusCode,
			Headers:    firstHeaderValues(response.Header),
			Stream:     &cancelOnCloseReader{ReadCloser: response.Body, cancel: cancel},
		}, nil
	}
	defer cancel()
	defer response.Body.Close()
	body, err := readHTTPResponseBody(response, c.maxBodyBytes())
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{StatusCode: response.StatusCode, Body: body, Headers: firstHeaderValues(response.Header)}, nil
}

func (c netHTTPClient) httpDoer() HTTPDoer {
	if c.Doer != nil {
		return c.Doer
	}
	return defaultNetHTTPDoer
}

func (c netHTTPClient) maxBodyBytes() int64 {
	if c.MaxBodyBytes > 0 {
		return c.MaxBodyBytes
	}
	return defaultMaxHTTPBodyBytes
}

func readHTTPResponseBody(response *http.Response, maxBytes int64) ([]byte, error) {
	body, err := readLimitedHTTPBody(response.Body, maxBytes)
	if err != nil {
		return nil, err
	}
	encoding := response.Header.Get("Content-Encoding")
	switch encoding {
	case "gzip":
		return decodeGzipBody(body, maxBytes)
	case "deflate":
		return decodeDeflateBody(body, maxBytes)
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		return decodeGzipBody(body, maxBytes)
	}
	return body, nil
}

func readLimitedHTTPBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func decodeGzipBody(body []byte, maxBytes int64) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode gzip response: %w", err)
	}
	defer reader.Close()
	decoded, err := readLimitedHTTPBody(reader, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("decode gzip response: %w", err)
	}
	return decoded, nil
}

func decodeDeflateBody(body []byte, maxBytes int64) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(body))
	defer reader.Close()
	decoded, err := readLimitedHTTPBody(reader, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("decode deflate response: %w", err)
	}
	return decoded, nil
}

func httpRequestURL(request HTTPRequest) string {
	if len(request.Params) == 0 {
		return request.URL
	}
	parsed, err := url.Parse(request.URL)
	if err != nil {
		return request.URL
	}
	query := parsed.Query()
	for key, value := range request.Params {
		query.Set(key, fmt.Sprint(value))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
