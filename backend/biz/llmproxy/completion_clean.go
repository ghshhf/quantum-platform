package llmproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
)

// markdownFencePattern matches code block fences like ```lang ... ```
// at the beginning or end of text content.
var markdownFenceStart = regexp.MustCompile(`^` + "```" + `\w*\n`)
var markdownFenceEnd = regexp.MustCompile(`\n` + "```" + `\s*$`)

// stripMarkdownFences removes leading/trailing markdown code fences from text.
func stripMarkdownFences(text string) string {
	text = markdownFenceStart.ReplaceAllString(text, "")
	text = markdownFenceEnd.ReplaceAllString(text, "")
	return text
}

// completionCleaner is an io.ReadCloser that strips markdown code fences
// from OpenAI completion-style responses (/v1/completions).
type completionCleaner struct {
	logger *slog.Logger
	buf    *bytes.Buffer
	done   bool
}

func newCompletionCleaner(logger *slog.Logger, src io.ReadCloser) *completionCleaner {
	cc := &completionCleaner{
		logger: logger,
		buf:    new(bytes.Buffer),
	}
	data, err := io.ReadAll(src)
	_ = src.Close()
	if err != nil {
		cc.logger.With("error", err).Warn("completionCleaner: read upstream body failed")
		cc.buf.Write(data) // pass through as-is
		return cc
	}
	cleaned := cleanCompletionBody(logger, data)
	cc.buf.Write(cleaned)
	return cc
}

// cleanCompletionBody parses OpenAI completion JSON, strips markdown fences
// from choices[*].text, and returns the cleaned JSON.
func cleanCompletionBody(logger *slog.Logger, data []byte) []byte {
	var body struct {
		Choices []struct {
			Text         string `json:"text"`
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason,omitempty"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		logger.With("error", err).Warn("completionCleaner: parse body failed, passing through")
		return data
	}
	changed := false
	for i := range body.Choices {
		cleaned := stripMarkdownFences(body.Choices[i].Text)
		if cleaned != body.Choices[i].Text {
			body.Choices[i].Text = cleaned
			changed = true
		}
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(body)
	if err != nil {
		logger.With("error", err).Warn("completionCleaner: marshal cleaned body failed, passing through")
		return data
	}
	return out
}

func (c *completionCleaner) Read(p []byte) (int, error) {
	if c.done {
		return 0, io.EOF
	}
	n, err := c.buf.Read(p)
	if err == io.EOF {
		c.done = true
		return n, io.EOF
	}
	return n, err
}

func (c *completionCleaner) Close() error {
	return nil
}
