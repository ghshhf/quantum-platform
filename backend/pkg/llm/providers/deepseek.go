package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

func init() {
	Register("deepseek", NewDeepSeek)
}

type DeepSeekProvider struct {
	client *openai.Client
	model  string
}

func NewDeepSeek(cfg Config) (Provider, error) {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	c := openai.DefaultConfig(cfg.APIKey)
	c.BaseURL = baseURL
	c.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	return &DeepSeekProvider{
		client: openai.NewClientWithConfig(c),
		model:  cfg.Model,
	}, nil
}

func (p *DeepSeekProvider) Name() string { return "deepseek" }

func (p *DeepSeekProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "deepseek-chat"
	}
	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model, Messages: msgs, MaxTokens: req.MaxTokens, Temperature: req.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("deepseek chat failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek: empty response")
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

func (p *DeepSeekProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 16)
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "deepseek-chat"
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

func (p *DeepSeekProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "deepseek-chat", Name: "DeepSeek Chat", Context: 65536, Owner: "deepseek"},
		{ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", Context: 65536, Owner: "deepseek"},
	}, nil
}
