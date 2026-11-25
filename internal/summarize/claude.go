package summarize

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Claude calls the Anthropic Messages API.
type Claude struct {
	client anthropic.Client
}

// NewClaude returns a Caller using the given API key.
func NewClaude(apiKey string) *Claude {
	return &Claude{client: anthropic.NewClient(option.WithAPIKey(apiKey))}
}

// Call sends one request. Refusals are routed to a fallback model server-side;
// a refusal that still comes back, or a truncated response, is an error.
func (c *Claude) Call(ctx context.Context, req Request) (string, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     req.Model,
		MaxTokens: max(req.MaxTokens, 16000),
		System: []anthropic.BetaTextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.User)),
		},
	}
	// Haiku has no server-side refusal fallback; Opus, Sonnet, and Fable do.
	if !strings.Contains(req.Model, "haiku") {
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(req.Effort)
	if req.Schema != nil {
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: req.Schema}
	}

	// Streamed and assembled here: the SDK refuses long outputs without
	// streaming.
	stream := c.client.Beta.Messages.NewStreaming(ctx, params)
	resp := &anthropic.BetaMessage{}
	for stream.Next() {
		if err := resp.Accumulate(stream.Current()); err != nil {
			_ = stream.Close()
			return "", fmt.Errorf("claude: %w", err)
		}
	}
	err := stream.Err()
	_ = stream.Close()
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			return "", fmt.Errorf("claude: HTTP %d: %w", apiErr.StatusCode, err)
		}
		return "", fmt.Errorf("claude: %w", err)
	}
	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return "", fmt.Errorf("claude: request declined (%s)", resp.StopDetails.Category)
	case anthropic.BetaStopReasonMaxTokens:
		return "", errors.New("claude: response hit max_tokens")
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", errors.New("claude: empty response")
	}
	return out, nil
}

// Check confirms the API key works by listing one model, which is free.
func (c *Claude) Check(ctx context.Context) error {
	if _, err := c.client.Models.List(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1)}); err != nil {
		return fmt.Errorf("claude: %w", err)
	}
	return nil
}
