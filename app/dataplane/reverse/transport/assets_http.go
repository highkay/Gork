package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	proxyadapters "github.com/dslzl/gork/app/dataplane/proxy/adapters"
	platform "github.com/dslzl/gork/app/platform"
)

type netHTTPAssetsClient struct{}

func (netHTTPAssetsClient) GetJSON(ctx context.Context, request AssetsHTTPRequest) (map[string]any, error) {
	response, err := doAssetsHTTPRequest(ctx, http.MethodGet, request, nil)
	if err != nil {
		return nil, err
	}
	return decodeAssetsJSON(response)
}

func (netHTTPAssetsClient) DeleteJSON(ctx context.Context, request AssetsHTTPRequest) (map[string]any, error) {
	response, err := doAssetsHTTPRequest(ctx, http.MethodDelete, request, nil)
	if err != nil {
		return nil, err
	}
	return decodeAssetsJSON(response)
}

func (netHTTPAssetsClient) GetBytesStream(ctx context.Context, request AssetsHTTPRequest) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	rawRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, assetsRequestURL(request), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	applyAssetsHeaders(rawRequest, request)
	response, err := defaultNetHTTPDoer.Do(rawRequest)
	if err != nil {
		cancel()
		return nil, err
	}
	if response.StatusCode != 200 {
		defer cancel()
		defer response.Body.Close()
		body, _ := readLimitedHTTPBody(response.Body, defaultMaxHTTPBodyBytes)
		return nil, platform.NewUpstreamError(fmt.Sprintf("HTTP %d", response.StatusCode), response.StatusCode, redactedTransportExcerpt(string(body)))
	}
	return &cancelOnCloseReader{ReadCloser: response.Body, cancel: cancel}, nil
}

func doAssetsHTTPRequest(ctx context.Context, method string, request AssetsHTTPRequest, body []byte) (AssetHTTPResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	rawRequest, err := http.NewRequestWithContext(ctx, method, assetsRequestURL(request), bytes.NewReader(body))
	if err != nil {
		return AssetHTTPResponse{}, err
	}
	applyAssetsHeaders(rawRequest, request)
	response, err := defaultNetHTTPDoer.Do(rawRequest)
	if err != nil {
		return AssetHTTPResponse{}, err
	}
	defer response.Body.Close()
	responseBody, err := readLimitedHTTPBody(response.Body, defaultMaxHTTPBodyBytes)
	if err != nil {
		return AssetHTTPResponse{}, err
	}
	if response.StatusCode != 200 {
		return AssetHTTPResponse{}, platform.NewUpstreamError(fmt.Sprintf("HTTP %d", response.StatusCode), response.StatusCode, redactedTransportExcerpt(string(responseBody)))
	}
	return AssetHTTPResponse{StatusCode: response.StatusCode, Body: responseBody, Headers: firstHeaderValues(response.Header)}, nil
}

func decodeAssetsJSON(response AssetHTTPResponse) (map[string]any, error) {
	if len(response.Body) == 0 {
		return map[string]any{}, nil
	}
	result := map[string]any{}
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func assetsRequestURL(request AssetsHTTPRequest) string {
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

func applyAssetsHeaders(rawRequest *http.Request, request AssetsHTTPRequest) {
	headers := proxyadapters.BuildHTTPHeaders(request.Token, proxyadapters.HTTPHeaderOptions{
		Lease:   request.Lease,
		Origin:  request.Origin,
		Referer: request.Referer,
	})
	for key, value := range request.ExtraHeaders {
		headers[key] = value
	}
	for key, value := range headers {
		rawRequest.Header.Set(key, value)
	}
}

type cancelOnCloseReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelOnCloseReader) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}
