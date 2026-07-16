package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

func TestAdapterDefaultModel(t *testing.T) {
	_, model, err := (Adapter{}).New(provider.AdapterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("model = %q", model)
	}
}

func TestClientRejectsInBandGenerationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"error":{"code":502,"message":"provider disconnected","metadata":{"error_type":"provider_unavailable"}},
			"choices":[{"finish_reason":"error","message":{"content":"{\"title\":\"partial\"}"}}]
		}`))
	}))
	defer server.Close()

	client := testClient(server)
	_, err := client.Complete(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "provider disconnected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRejectsErrorFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices":[{"finish_reason":"error","message":{"content":"{\"title\":\"partial\"}"}}]
		}`))
	}))
	defer server.Close()

	client := testClient(server)
	_, err := client.Complete(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "ended with an error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientEscapesRemoteErrorText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusBadGateway)
		_, _ = response.Write([]byte("bad\x1b]52;c;Y2xpcGJvYXJk\a"))
	}))
	defer server.Close()

	client := testClient(server)
	_, err := client.Complete(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "\x1b") || !strings.Contains(err.Error(), `\x1b`) {
		t.Fatalf("remote control characters were not escaped: %q", err)
	}
}

func TestClientEscapesConfiguredEnvironmentName(t *testing.T) {
	client := &Client{
		APIKeyEnv: "KEY\x1b]52;c;YQ==\a",
		Getenv:    func(string) string { return "" },
	}
	available, detail := client.Detect()
	if available {
		t.Fatal("provider should not be available")
	}
	if strings.Contains(detail, "\x1b") || !strings.Contains(detail, `\x1b`) {
		t.Fatalf("environment name was not escaped: %q", detail)
	}
}

func TestStructuredRequestRequiresSupportedParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		providerOptions, ok := body["provider"].(map[string]any)
		if !ok || providerOptions["require_parameters"] != true {
			t.Errorf("structured request did not require parameter support: %#v", body)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices":[{"finish_reason":"stop","message":{"content":"{\"title\":\"ok\"}"}}]
		}`))
	}))
	defer server.Close()

	client := testClient(server)
	if _, err := client.CompleteStructured(context.Background(), "prompt", json.RawMessage(`{"type":"object"}`)); err != nil {
		t.Fatal(err)
	}
}

func testClient(server *httptest.Server) *Client {
	return &Client{
		Model: "test", APIKeyEnv: "TEST_KEY", BaseURL: server.URL,
		Getenv:     func(string) string { return "secret" },
		HTTPClient: server.Client(),
	}
}
