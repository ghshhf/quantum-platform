package providers

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

func init() {
	Register("anthropic", NewAnthropic)
}

type AnthropicProvider struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewAnthropic(cfg Config) (Provider, error) {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
	}, nil
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("anthropic: model is required")
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	var system string
	msgs := make([]map[string]interface{}, 0, len(req.Messages))
	if req.System != "" {
		system = req.System
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system == "" {
				system = m.Content
			} else {
				system += "\n" + m.Content
			}
			continue
		}
		msgs = append(msgs, map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	body := map[string]interface{}{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTokens,
	}
	if system != "" {
		body["system"] = system
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	b, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: %s, %s", resp.Status, string(b))
	}
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return &ChatResponse{
		Content: sb.String(),
		Usage: Usage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	resp, err := p.Chat(ctx, req)
	ch := make(chan StreamChunk, 4)
	if err != nil {
		ch <- StreamChunk{Error: err.Error(), Done: true}
		close(ch)
		return ch, nil
	}
	go func() {
		defer close(ch)
		ch <- StreamChunk{Delta: resp.Content}
		ch <- StreamChunk{Done: true}
	}()
	return ch, nil
}

func (p *AnthropicProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "claude-3-opus-20240229", Name: "Claude 3 Opus", Context: 200000, Owner: "anthropic"},
		{ID: "claude-3-sonnet-20240229", Name: "Claude 3 Sonnet", Context: 200000, Owner: "anthropic"},
		{ID: "claude-3-haiku-20240307", Name: "Claude 3 Haiku", Context: 200000, Owner: "anthropic"},
		{ID: "claude-3-5-sonnet-20240620", Name: "Claude 3.5 Sonnet", Context: 200000, Owner: "anthropic"},
		{ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku", Context: 200000, Owner: "anthropic"},
	}, nil
}
