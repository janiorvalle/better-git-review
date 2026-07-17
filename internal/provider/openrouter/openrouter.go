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

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, string, []string, error) {
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "z-ai/glm-5.2")
	reasoning := provider.ChooseReasoning(opts.ReasoningOverride, opts.ConfiguredReasoning, "")
	if err := provider.ValidateReasoning("openrouter", reasoning, openRouterReasoningLevels(model)...); err != nil {
		return nil, "", "", nil, err
	}
	return &Client{
		Model:     model,
		Reasoning: reasoning,
		APIKeyEnv: provider.DefaultString(opts.APIKeyEnv, "OPENROUTER_API_KEY"),
		BaseURL:   provider.DefaultString(opts.BaseURL, "https://openrouter.ai/api/v1"),
		Getenv:    opts.Getenv,
	}, model, reasoning, nil, nil
}

type Client struct {
	Model            string
	Reasoning        string
	APIKeyEnv        string
	BaseURL          string
	Getenv           func(string) string
	HTTPClient       *http.Client
	reasoningByModel map[string][]string
	contextByModel   map[string]int
	catalogLoaded    bool
}

func (p *Client) Name() string { return "openrouter" }

func (p *Client) Models(ctx context.Context) ([]provider.ModelOption, error) {
	p.catalogLoaded = false
	p.reasoningByModel = nil
	p.contextByModel = nil
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return CuratedModels(), nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CuratedModels(), nil
	}
	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			Reasoning struct {
				SupportedEfforts json.RawMessage `json:"supported_efforts"`
			} `json:"reasoning"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&payload); err != nil {
		return CuratedModels(), nil
	}
	result := make([]provider.ModelOption, 0, len(payload.Data))
	p.reasoningByModel = map[string][]string{}
	p.contextByModel = map[string]int{}
	for _, model := range payload.Data {
		if model.ID == "" {
			continue
		}
		note := catalogNote(model.Pricing.Prompt, model.Pricing.Completion, model.ContextLength)
		result = append(result, provider.ModelOption{
			ID: model.ID, Label: provider.DefaultString(model.Name, model.ID), Note: note,
			Default: model.ID == "z-ai/glm-5.2",
		})
		if model.ContextLength > 0 {
			p.contextByModel[model.ID] = model.ContextLength
		}
		rawEfforts := model.Reasoning.SupportedEfforts
		switch {
		case len(rawEfforts) == 0:
			// Omitted means this model does not expose effort selection.
		case strings.TrimSpace(string(rawEfforts)) == "null":
			p.reasoningByModel[model.ID] = gatewayReasoningLevels()
		default:
			var efforts []string
			if json.Unmarshal(rawEfforts, &efforts) == nil && len(efforts) > 0 {
				p.reasoningByModel[model.ID] = efforts
			}
		}
	}
	if len(result) == 0 {
		return CuratedModels(), nil
	}
	p.catalogLoaded = true
	return result, nil
}

func (p *Client) AnalysisBudget(ctx context.Context) int {
	if p.contextByModel == nil {
		_, _ = p.Models(ctx)
	}
	if contextLength := p.contextByModel[p.Model]; contextLength > 0 {
		// 3.5 chars/token at 60% of context, rounded down to 10k chars.
		budget := int((int64(contextLength) * 21 / 10) / 10_000 * 10_000)
		if budget > 0 {
			return budget
		}
	}
	switch p.Model {
	case "z-ai/glm-5.2":
		return 2_000_000
	case "anthropic/claude-sonnet-4.5":
		return 400_000
	default:
		return provider.DefaultAnalysisBudget
	}
}

func (p *Client) ReasoningLevels() []string {
	if p.catalogLoaded {
		return append([]string(nil), p.reasoningByModel[p.Model]...)
	}
	if levels := p.reasoningByModel[p.Model]; len(levels) > 0 {
		return append([]string(nil), levels...)
	}
	return openRouterReasoningLevels(p.Model)
}

func (p *Client) SetCatalogModel(model string) { p.Model = model }

func CuratedModels() []provider.ModelOption {
	return []provider.ModelOption{
		{ID: "z-ai/glm-5.2", Label: "GLM 5.2", Note: "recommended", Default: true},
		{ID: "anthropic/claude-sonnet-4.5", Label: "Claude Sonnet 4.5", Note: "fallback"},
	}
}

func openRouterReasoningLevels(model string) []string {
	if model == "z-ai/glm-5.2" {
		return []string{"xhigh", "high"}
	}
	return gatewayReasoningLevels()
}

func gatewayReasoningLevels() []string {
	return []string{"max", "xhigh", "high", "medium", "low", "minimal", "none"}
}

func catalogNote(promptPrice, completionPrice string, contextLength int) string {
	price := func(raw string) string {
		var value float64
		if _, err := fmt.Sscan(raw, &value); err != nil {
			return "?"
		}
		return fmt.Sprintf("$%.2f", value*1_000_000)
	}
	context := fmt.Sprintf("%dk ctx", contextLength/1000)
	if contextLength >= 1_000_000 {
		context = fmt.Sprintf("%.1fM ctx", float64(contextLength)/1_000_000)
	}
	return fmt.Sprintf("%s/%s per M in/out, %s", price(promptPrice), price(completionPrice), context)
}

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
	if p.Reasoning != "" {
		requestBody["reasoning"] = map[string]string{"effort": p.Reasoning}
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
