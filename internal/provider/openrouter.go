package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenRouter struct {
	Model     string
	APIKeyEnv string
	BaseURL   string
	Getenv    func(string) string
	Client    *http.Client
}

func (p *OpenRouter) Name() string { return "openrouter" }

func (p *OpenRouter) Detect() (bool, string) {
	if p.Getenv == nil {
		return false, "environment reader is not configured"
	}
	if p.Getenv(p.APIKeyEnv) == "" {
		return false, fmt.Sprintf("%s is not set", p.APIKeyEnv)
	}
	return true, fmt.Sprintf("%s is set", p.APIKeyEnv)
}

func (p *OpenRouter) Complete(ctx context.Context, prompt string) (string, error) {
	content, err := p.complete(ctx, prompt, nil)
	return string(content), err
}

func (p *OpenRouter) CompleteStructured(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error) {
	return p.complete(ctx, prompt, schema)
}

func (p *OpenRouter) complete(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error) {
	if p.Getenv == nil {
		return nil, fmt.Errorf("environment reader is not configured")
	}
	apiKey := p.Getenv(p.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s is not set", p.APIKeyEnv)
	}
	requestBody := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if schema != nil {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter returned %s: %.500s", response.Status, body)
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("parse openrouter response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("openrouter response contained no choices")
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
