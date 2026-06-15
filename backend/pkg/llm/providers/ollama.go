package providers

import (
	"bufio"
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
	Register("ollama", NewOllama)
}

type OllamaProvider struct {
	httpClient *http.Client
	baseURL    string
	model      string
}

func NewOllama(cfg Config) (Provider, error) {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	return &OllamaProvider{
		httpClient: &http.Client{Timeout: 300 * time.Second},
		baseURL:    baseURL,
		model:      cfg.Model,
	}, nil
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("ollama: model is required (try 'ollama pull qwen2.5:7b' first)")
	}
	body := struct {
		Model    string              `json:"model"`
		Messages []map[string]string `json:"messages"`
		Stream   bool                `json:"stream"`
	}{
		Model: model, Stream: false,
	}
	body.Messages = make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		body.Messages = append(body.Messages, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	b, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama chat: %s, %s", resp.Status, string(b))
	}
	var ollResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done         bool `json:"done"`
		EvalCount    int  `json:"eval_count"`
		PromptEvalCount int `json:"prompt_eval_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollResp); err != nil {
		return nil, err
	}
	return &ChatResponse{
		Content: ollResp.Message.Content,
		Usage: Usage{
			PromptTokens:     ollResp.PromptEvalCount,
			CompletionTokens: ollResp.EvalCount,
			TotalTokens:      ollResp.PromptEvalCount + ollResp.EvalCount,
		},
	}, nil
}

func (p *OllamaProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 16)
	model := req.Model
	if model == "" {
		model = p.model
	}
	body := struct {
		Model    string              `json:"model"`
		Messages []map[string]string `json:"messages"`
		Stream   bool                `json:"stream"`
	}{
		Model: model, Stream: true,
	}
	body.Messages = make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		body.Messages = append(body.Messages, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	b, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		close(ch)
		return ch, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		close(ch)
		return ch, fmt.Errorf("ollama stream failed: %w", err)
	}
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var chunk struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := json.Unmarshal(line, &chunk); err == nil {
				if chunk.Done {
					ch <- StreamChunk{Done: true}
					return
				}
				ch <- StreamChunk{Delta: chunk.Message.Content}
			}
		}
	}()
	return ch, nil
}

func (p *OllamaProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama list failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama list: %s, %s", resp.Status, string(b))
	}
	var result struct {
		Models []struct {
			Name       string `json:"name"`
			Model      string `json:"model"`
			ModifiedAt string `json:"modified_at"`
			Size       int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(result.Models))
	for _, m := range result.Models {
		out = append(out, ModelInfo{ID: m.Name, Name: m.Name, Owner: "ollama-local"})
	}
	return out, nil
}
