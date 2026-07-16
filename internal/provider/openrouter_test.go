package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterRejectsInBandGenerationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"error":{"code":502,"message":"provider disconnected","metadata":{"error_type":"provider_unavailable"}},
			"choices":[{"finish_reason":"error","message":{"content":"{\"title\":\"partial\"}"}}]
		}`))
	}))
	defer server.Close()

	provider := &OpenRouter{
		Model: "test", APIKeyEnv: "TEST_KEY", BaseURL: server.URL,
		Getenv: func(string) string { return "secret" },
		Client: server.Client(),
	}
	_, err := provider.Complete(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "provider disconnected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenRouterRejectsErrorFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices":[{"finish_reason":"error","message":{"content":"{\"title\":\"partial\"}"}}]
		}`))
	}))
	defer server.Close()

	provider := &OpenRouter{
		Model: "test", APIKeyEnv: "TEST_KEY", BaseURL: server.URL,
		Getenv: func(string) string { return "secret" },
		Client: server.Client(),
	}
	_, err := provider.Complete(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "ended with an error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenRouterEscapesRemoteErrorText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusBadGateway)
		_, _ = response.Write([]byte("bad\x1b]52;c;Y2xpcGJvYXJk\a"))
	}))
	defer server.Close()

	provider := &OpenRouter{
		Model: "test", APIKeyEnv: "TEST_KEY", BaseURL: server.URL,
		Getenv: func(string) string { return "secret" },
		Client: server.Client(),
	}
	_, err := provider.Complete(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "\x1b") || !strings.Contains(err.Error(), `\x1b`) {
		t.Fatalf("remote control characters were not escaped: %q", err)
	}
}

func TestOpenRouterEscapesConfiguredEnvironmentName(t *testing.T) {
	provider := &OpenRouter{
		APIKeyEnv: "KEY\x1b]52;c;YQ==\a",
		Getenv:    func(string) string { return "" },
	}
	available, detail := provider.Detect()
	if available {
		t.Fatal("provider should not be available")
	}
	if strings.Contains(detail, "\x1b") || !strings.Contains(detail, `\x1b`) {
		t.Fatalf("environment name was not escaped: %q", detail)
	}
}
