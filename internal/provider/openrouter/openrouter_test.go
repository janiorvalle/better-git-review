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
	_, model, _, _, err := (Adapter{}).New(provider.AdapterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if model != "z-ai/glm-5.2" {
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

func TestReasoningIsIncludedInRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "high" {
			t.Errorf("reasoning body = %#v", body)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	client := testClient(server)
	client.Reasoning = "high"
	if _, err := client.Complete(context.Background(), "prompt"); err != nil {
		t.Fatal(err)
	}
}

func TestLiveCatalogShapeAndPerModelReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":[{
			"id":"z-ai/glm-5.2","name":"Z.ai: GLM 5.2","context_length":1048576,
			"pricing":{"prompt":"0.0000009016","completion":"0.0000028336"},
			"reasoning":{"supported_efforts":["xhigh","high"]}
		}]}`))
	}))
	defer server.Close()
	client := testClient(server)
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "z-ai/glm-5.2" || !strings.Contains(models[0].Note, "$0.90/$2.83") || !strings.Contains(models[0].Note, "1.0M") {
		t.Fatalf("models = %#v", models)
	}
	client.SetCatalogModel("z-ai/glm-5.2")
	levels := client.ReasoningLevels()
	if len(levels) != 2 || levels[0] != "xhigh" || levels[1] != "high" {
		t.Fatalf("levels = %#v", levels)
	}
}

func TestCatalogFallsBackWhenRemoteIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := testClient(server)
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 || models[0].ID != "z-ai/glm-5.2" || !models[0].Default {
		t.Fatalf("fallback models = %#v", models)
	}
}

func TestLiveCatalogDoesNotInventReasoningForUnsupportedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":[{"id":"example/no-reasoning","name":"No Reasoning","context_length":1000,"pricing":{}}]}`))
	}))
	defer server.Close()
	client := testClient(server)
	if _, err := client.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.SetCatalogModel("example/no-reasoning")
	if levels := client.ReasoningLevels(); len(levels) != 0 {
		t.Fatalf("levels = %#v", levels)
	}
}

func testClient(server *httptest.Server) *Client {
	return &Client{
		Model: "test", APIKeyEnv: "TEST_KEY", BaseURL: server.URL,
		Getenv:     func(string) string { return "secret" },
		HTTPClient: server.Client(),
	}
}
