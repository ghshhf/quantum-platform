package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

func init() {
	Register("openai", NewOpenAI)
	Register("custom", NewOpenAI)
}

// OpenAIProvider 支持任何 OpenAI 兼容协议
type OpenAIProvider struct {
	client     *openai.Client
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewOpenAI(cfg Config) (Provider, error) {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	c := openai.DefaultConfig(cfg.APIKey)
	if baseURL != "" {
		c.BaseURL = baseURL
	}
	return &OpenAIProvider{
		client:     openai.NewClientWithConfig(c),
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
	}, nil
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("openai: model is required")
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	temp := req.Temperature
	if temp == 0 {
		temp = 0.7
	}
	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    msgs,
		MaxTokens:   maxTokens,
		Temperature: temp,
	})
	if err != nil {
		return nil, fmt.Errorf("openai chat failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty response")
	}
	return &ChatResponse{
		Content: resp.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

func (p *OpenAIProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 16)
	model := req.Model
	if model == "" {
		model = p.model
	}
	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}
	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: model, Messages: msgs, MaxTokens: req.MaxTokens, Temperature: req.Temperature, Stream: true,
	})
	if err != nil {
		close(ch)
		return ch, err
	}
	go func() {
		defer close(ch)
		defer stream.Close()
		for {
			part, err := stream.Recv()
			if err == io.EOF {
				ch <- StreamChunk{Done: true}
				return
			}
			if err != nil {
				ch <- StreamChunk{Error: err.Error(), Done: true}
				return
			}
			if len(part.Choices) > 0 {
				ch <- StreamChunk{Delta: part.Choices[0].Delta.Content}
			}
		}
	}()
	return ch, nil
}

func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	url := strings.TrimSuffix(p.baseURL, "/v1") + "/v1/models"
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai list models failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai list models: %s, %s", resp.Status, string(body))
	}
	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(result.Data))
	for _, d := range result.Data {
		out = append(out, ModelInfo{ID: d.ID, Name: d.ID, Owner: d.OwnedBy})
	}
	return out, nil
}
