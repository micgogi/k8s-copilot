// Package agent wires the Claude API client and tools into a debugging loop.
package agent

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/micgogi/k8s-copilot/cli/internal/anthropic"
	"github.com/micgogi/k8s-copilot/cli/internal/tools"
)

const (
	defaultModel = "claude-sonnet-4-6"
	maxTokens    = 4096
)

const systemPrompt = `You are k8s-copilot, an expert Kubernetes debugging assistant.

When given a namespace (and optionally a specific service), investigate the
state of the cluster using the tools available and produce a ranked list of
likely root causes for any unhealthy state. Cite specific evidence for each
claim.

Rules:
- Read-only. You observe but never mutate the cluster.
- Cite evidence: every claim must reference a specific pod, condition, or value.
- Be concise. Engineers reading you are usually mid-incident.
- If everything looks healthy, say so plainly. Do not invent problems.
- Prefer concrete next-step commands over vague advice
  ("kubectl describe pod X" beats "investigate further").

Output format for your final answer:
  1. Summary — one sentence verdict.
  2. Findings — bullet list, severity-ordered, each with cited evidence.
  3. Suggested commands — kubectl commands the human should run next.`

// DiagnoseInput controls a single diagnosis run.
type DiagnoseInput struct {
	Service   string
	Namespace string
}

// Result is what the agent returns for both the live CLI path and evals.
type Result struct {
	FinalText string
	Usage     anthropic.Usage
}

// Diagnose runs the single-turn agent loop against the default kubectl runner
// and prints to stdout/stderr. This is the live CLI entry point.
func Diagnose(ctx context.Context, in DiagnoseInput) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}
	if in.Namespace == "" {
		in.Namespace = "default"
	}

	client := anthropic.NewClient(apiKey)
	runner := tools.NewKubectlRunner(in.Namespace)

	fmt.Fprintln(os.Stderr, "→ kcp: investigating...")
	res, err := Run(ctx, client, runner, in, os.Stderr)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("─── diagnosis ───")
	fmt.Println(res.FinalText)
	fmt.Fprintf(os.Stderr,
		"\n(tokens: in=%d out=%d, cache_read=%d, cache_create=%d)\n",
		res.Usage.InputTokens, res.Usage.OutputTokens,
		res.Usage.CacheReadInputTokens, res.Usage.CacheCreationInputTokens,
	)
	return nil
}

// Run executes one diagnosis turn against an arbitrary Runner. Used by both
// the live CLI and the eval harness.
//
// Single-turn for now: model gets one chance to invoke tools, then must
// produce the final answer.
func Run(ctx context.Context, client *anthropic.Client, runner tools.Runner, in DiagnoseInput, trace io.Writer) (*Result, error) {
	if trace == nil {
		trace = io.Discard
	}
	if in.Namespace == "" {
		in.Namespace = "default"
	}

	userText := fmt.Sprintf("Investigate the cluster in namespace %q.", in.Namespace)
	if in.Service != "" {
		userText += fmt.Sprintf(" Focus on the service %q.", in.Service)
	}

	req := anthropic.Request{
		Model:     defaultModel,
		MaxTokens: maxTokens,
		System: []anthropic.SystemBlock{
			{Type: "text", Text: systemPrompt, CacheControl: anthropic.Ephemeral()},
		},
		Tools: runner.Tools(),
		Messages: []anthropic.Message{
			{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: userText}}},
		},
	}

	resp, err := client.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("anthropic call: %w", err)
	}

	totalUsage := resp.Usage

	if resp.StopReason == "tool_use" {
		assistantBlocks := append([]anthropic.ContentBlock(nil), resp.Content...)
		var toolResults []anthropic.ContentBlock

		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			fmt.Fprintf(trace, "→ tool: %s\n", block.Name)
			result, isErr := runner.Run(ctx, block.Name, block.Input)
			toolResults = append(toolResults, anthropic.ContentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result,
				IsError:   isErr,
			})
		}

		req.Messages = append(req.Messages,
			anthropic.Message{Role: "assistant", Content: assistantBlocks},
			anthropic.Message{Role: "user", Content: toolResults},
		)

		resp, err = client.Send(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("anthropic follow-up call: %w", err)
		}
		totalUsage.InputTokens += resp.Usage.InputTokens
		totalUsage.OutputTokens += resp.Usage.OutputTokens
		totalUsage.CacheCreationInputTokens += resp.Usage.CacheCreationInputTokens
		totalUsage.CacheReadInputTokens += resp.Usage.CacheReadInputTokens
	}

	var finalText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			finalText += block.Text
		}
	}
	return &Result{FinalText: finalText, Usage: totalUsage}, nil
}
