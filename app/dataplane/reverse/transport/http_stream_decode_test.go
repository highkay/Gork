package transport

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPostStreamDecodesGzipLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		if _, err := writer.Write([]byte("data: one\n\ndata: two\n")); err != nil {
			t.Fatalf("write gzip stream: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close gzip stream: %v", err)
		}
	}))
	defer server.Close()

	stream, err := PostStream(context.Background(), server.URL, "token", []byte("payload"))
	if err != nil {
		t.Fatalf("PostStream returned error: %v", err)
	}
	got := drainHTTPLineStream(t, stream)
	if !reflect.DeepEqual(got, []string{"data: one", "", "data: two"}) {
		t.Fatalf("stream lines = %#v", got)
	}
}

func TestPostStreamDecodesDeflateLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		writer, err := flate.NewWriter(w, flate.DefaultCompression)
		if err != nil {
			t.Fatalf("create deflate writer: %v", err)
		}
		if _, err := writer.Write([]byte("data: one\n\ndata: two\n")); err != nil {
			t.Fatalf("write deflate stream: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close deflate stream: %v", err)
		}
	}))
	defer server.Close()

	stream, err := PostStream(context.Background(), server.URL, "token", []byte("payload"))
	if err != nil {
		t.Fatalf("PostStream returned error: %v", err)
	}
	got := drainHTTPLineStream(t, stream)
	if !reflect.DeepEqual(got, []string{"data: one", "", "data: two"}) {
		t.Fatalf("stream lines = %#v", got)
	}
}
