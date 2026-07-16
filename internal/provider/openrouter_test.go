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
