package redisrest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDoSendsCommandAndParsesResult(t *testing.T) {
	var gotAuth string
	var gotBody []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/" {
			t.Fatalf("path = %q, want /", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"result":"OK"}`))
	}))
	defer server.Close()

	client, err := New(Config{URL: server.URL + "/", Token: "token"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	result, err := client.Do(context.Background(), "SET", "key", "value", "NX")
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if result != "OK" {
		t.Fatalf("result = %#v, want OK", result)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if len(gotBody) != 4 || gotBody[0] != "SET" || gotBody[3] != "NX" {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestClientPipelineUsesExpectedEndpoint(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body [][]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode body: %v", err)
		}
		if len(body) != 2 || body[0][0] != "DEL" || body[1][0] != "INCR" {
			t.Fatalf("pipeline body = %#v", body)
		}
		_, _ = w.Write([]byte(`[{"result":1},{"result":2}]`))
	}))
	defer server.Close()

	client, err := New(Config{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	_, err = client.Pipeline(context.Background(), [][]any{{"DEL", "a"}, {"INCR", "b"}}, false)
	if err != nil {
		t.Fatalf("Pipeline returned error: %v", err)
	}
	_, err = client.Pipeline(context.Background(), [][]any{{"DEL", "a"}, {"INCR", "b"}}, true)
	if err != nil {
		t.Fatalf("transaction Pipeline returned error: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/pipeline" || paths[1] != "/multi-exec" {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestClientErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"ERR bad command"}`))
	}))
	defer server.Close()

	client, err := New(Config{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	_, err = client.Do(context.Background(), "BAD")
	if err == nil || err.Error() != "Upstash Redis REST error: ERR bad command" {
		t.Fatalf("Do error = %v", err)
	}
}
