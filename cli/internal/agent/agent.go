// Package agent wires the Claude API client and tools into a debugging loop.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
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

// Diagnose runs a single-turn agent: send user prompt + tools, execute any
// tool calls once, send tool results back, print the final answer.
func Diagnose(ctx context.Context, in DiagnoseInput) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}
	if in.Namespace == "" {
		in.Namespace = "default"
	}

	client := anthropic.NewClient(apiKey)

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
		Tools: []anthropic.Tool{
			{
				Name:         "kubectl_get_pods",
				Description:  "List pods in a Kubernetes namespace with their status, ready state, restart counts, and notable conditions. Read-only.",
				InputSchema:  tools.GetPodsSchema(),
				CacheControl: anthropic.Ephemeral(),
			},
		},
		Messages: []anthropic.Message{
			{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: userText}}},
		},
	}

	fmt.Fprintln(os.Stderr, "→ kcp: investigating...")
	resp, err := client.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("anthropic call: %w", err)
	}

	if resp.StopReason == "tool_use" {
		assistantBlocks := append([]anthropic.ContentBlock(nil), resp.Content...)
		var toolResults []anthropic.ContentBlock

		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			fmt.Fprintf(os.Stderr, "→ tool: %s\n", block.Name)
			result, isErr := runTool(ctx, block, in.Namespace)
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
			return fmt.Errorf("anthropic follow-up call: %w", err)
		}
	}

	fmt.Println()
	fmt.Println("─── diagnosis ───")
	for _, block := range resp.Content {
		if block.Type == "text" {
			fmt.Println(block.Text)
		}
	}
	fmt.Fprintf(os.Stderr,
		"\n(tokens: in=%d out=%d, cache_read=%d, cache_create=%d)\n",
		resp.Usage.InputTokens, resp.Usage.OutputTokens,
		resp.Usage.CacheReadInputTokens, resp.Usage.CacheCreationInputTokens,
	)
	return nil
}

// runTool dispatches a single tool_use block and returns its string result.
func runTool(ctx context.Context, block anthropic.ContentBlock, defaultNS string) (string, bool) {
	switch block.Name {
	case "kubectl_get_pods":
		var input tools.GetPodsInput
		if err := json.Unmarshal(block.Input, &input); err != nil {
			return fmt.Sprintf("error parsing tool input: %v", err), true
		}
		if input.Namespace == "" {
			input.Namespace = defaultNS
		}
		out, err := tools.GetPods(ctx, input)
		if err != nil {
			return err.Error(), true
		}
		return out, false
	default:
		return fmt.Sprintf("unknown tool: %s", block.Name), true
	}
}
