package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

type Adapter struct{}

func (Adapter) Name() string {
	return "openrouter"
}

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, error) {
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "anthropic/claude-sonnet-4.5")
	return &Client{
		Model:     model,
		APIKeyEnv: provider.DefaultString(opts.APIKeyEnv, "OPENROUTER_API_KEY"),
		BaseURL:   provider.DefaultString(opts.BaseURL, "https://openrouter.ai/api/v1"),
		Getenv:    opts.Getenv,
	}, model, nil
}

type Client struct {
	Model      string
	APIKeyEnv  string
	BaseURL    string
	Getenv     func(string) string
	HTTPClient *http.Client
}

func (p *Client) Name() string { return "openrouter" }

func (p *Client) Detect() (bool, string) {
	if p.Getenv == nil {
		return false, "environment reader is not configured"
	}
	if p.Getenv(p.APIKeyEnv) == "" {
		return false, fmt.Sprintf("%s is not set", provider.SafeDiagnostic(p.APIKeyEnv, 200))
	}
	return true, fmt.Sprintf("%s is set", provider.SafeDiagnostic(p.APIKeyEnv, 200))
}

func (p *Client) Complete(ctx context.Context, prompt string) (string, error) {
	content, err := p.complete(ctx, prompt, nil)
	return string(content), err
}

func (p *Client) CompleteStructured(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error) {
	return p.complete(ctx, prompt, schema)
}

func (p *Client) complete(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error) {
	if p.Getenv == nil {
		return nil, fmt.Errorf("environment reader is not configured")
	}
	apiKey := p.Getenv(p.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s is not set", provider.SafeDiagnostic(p.APIKeyEnv, 200))
	}
	requestBody := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if schema != nil {
		requestBody["provider"] = map[string]any{
			"require_parameters": true,
		}
		requestBody["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "better_git_review_analysis",
				"strict": true,
				"schema": json.RawMessage(schema),
			},
		}
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter returned %s: %s", response.Status, provider.SafeDiagnostic(string(body), 500))
	}
	var decoded struct {
		Error *struct {
			Code     json.RawMessage        `json:"code"`
			Message  string                 `json:"message"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("parse openrouter response: %w", err)
	}
	if decoded.Error != nil {
		return nil, fmt.Errorf("openrouter generation error: %s", provider.SafeDiagnostic(decoded.Error.Message, 500))
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("openrouter response contained no choices")
	}
	if decoded.Choices[0].FinishReason == "error" {
		return nil, fmt.Errorf("openrouter generation ended with an error")
	}
	content := decoded.Choices[0].Message.Content
	var contentString string
	if err := json.Unmarshal(content, &contentString); err == nil {
		return json.RawMessage(contentString), nil
	}
	if json.Valid(content) {
		return content, nil
	}
	return nil, fmt.Errorf("openrouter response content was not JSON")
}
