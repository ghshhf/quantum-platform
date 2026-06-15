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
	Register("gemini", NewGemini)
}

type GeminiProvider struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewGemini(cfg Config) (Provider, error) {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	return &GeminiProvider{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
	}, nil
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("gemini: model is required")
	}
	contents := make([]map[string]interface{}, 0, len(req.Messages))
	var systemText string
	if req.System != "" {
		systemText = req.System
	}
	for _, m := range req.Messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			systemText += "\n" + m.Content
			continue
		}
		contents = append(contents, map[string]interface{}{
			"role": role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}
	body := map[string]interface{}{"contents": contents}
	if systemText != "" {
		body["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": systemText}},
		}
	}
	b, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bb, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini: %s, %s", resp.Status, string(bb))
	}
	var apiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: empty response (可能触发安全限制)")
	}
	var sb strings.Builder
	for _, part := range apiResp.Candidates[0].Content.Parts {
		sb.WriteString(part.Text)
	}
	return &ChatResponse{Content: sb.String(), Usage: Usage{}}, nil
}

func (p *GeminiProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
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

func (p *GeminiProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/v1beta/models?key=%s", p.baseURL, p.apiKey), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini list failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini list: %s, %s", resp.Status, string(b))
	}
	var result struct {
		Models []struct {
			Name               string `json:"name"`
			DisplayName        string `json:"displayName"`
			InputTokenLimit    int    `json:"inputTokenLimit"`
			OutputTokenLimit   int    `json:"outputTokenLimit"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(result.Models))
	for _, m := range result.Models {
		id := m.Name
		if strings.HasPrefix(id, "models/") {
			id = strings.TrimPrefix(id, "models/")
		}
		out = append(out, ModelInfo{ID: id, Name: m.DisplayName, Context: m.InputTokenLimit, Owner: "google"})
	}
	return out, nil
}
