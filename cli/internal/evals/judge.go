package evals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/micgogi/k8s-copilot/cli/internal/anthropic"
)

const (
	judgeModel     = "claude-haiku-4-5-20251001"
	judgeMaxTokens = 512
)

const judgeSystem = `You are an evaluator scoring a Kubernetes debugging assistant.

You receive: (1) the GROUND TRUTH root cause of an incident, (2) the assistant's
diagnosis text.

Decide whether the assistant correctly identified the ground-truth root cause
as the primary issue. Pass if the diagnosis names the same underlying problem,
even if wording differs (e.g. "ImagePullBackOff" and "image pull failure" are
the same cause). Fail if the assistant missed it, identified a different
primary cause, or hedged so vaguely the SRE wouldn't act on it.

Call the submit_score tool exactly once with your verdict.`

// Verdict is the judge's structured output.
type Verdict struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

// Judge wraps an anthropic.Client and scores diagnoses against ground truth.
type Judge struct {
	client *anthropic.Client
}

// NewJudge builds a Judge backed by the given client.
func NewJudge(client *anthropic.Client) *Judge { return &Judge{client: client} }

var submitScoreSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pass": {
      "type": "boolean",
      "description": "true if the diagnosis correctly identifies the ground-truth root cause as the primary issue."
    },
    "reason": {
      "type": "string",
      "description": "One sentence justifying the verdict. If failing, name what the assistant missed or got wrong."
    }
  },
  "required": ["pass", "reason"]
}`)

// Score asks Haiku whether actual matches expected. Returns a Verdict and
// the token usage of the judge call.
func (j *Judge) Score(ctx context.Context, expected, actual string) (Verdict, anthropic.Usage, error) {
	userText := fmt.Sprintf(
		"GROUND TRUTH:\n%s\n\nASSISTANT DIAGNOSIS:\n%s",
		expected, actual,
	)

	req := anthropic.Request{
		Model:     judgeModel,
		MaxTokens: judgeMaxTokens,
		System: []anthropic.SystemBlock{
			{Type: "text", Text: judgeSystem, CacheControl: anthropic.Ephemeral()},
		},
		Tools: []anthropic.Tool{
			{
				Name:         "submit_score",
				Description:  "Submit the pass/fail verdict for this diagnosis.",
				InputSchema:  submitScoreSchema,
				CacheControl: anthropic.Ephemeral(),
			},
		},
		Messages: []anthropic.Message{
			{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: userText}}},
		},
	}

	resp, err := j.client.Send(ctx, req)
	if err != nil {
		return Verdict{}, anthropic.Usage{}, fmt.Errorf("judge call: %w", err)
	}

	for _, block := range resp.Content {
		if block.Type != "tool_use" || block.Name != "submit_score" {
			continue
		}
		var v Verdict
		if err := json.Unmarshal(block.Input, &v); err != nil {
			return Verdict{}, resp.Usage, fmt.Errorf("parse judge verdict: %w", err)
		}
		return v, resp.Usage, nil
	}
	return Verdict{}, resp.Usage, fmt.Errorf("judge did not call submit_score (stop_reason=%s)", resp.StopReason)
}
