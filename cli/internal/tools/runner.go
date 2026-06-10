package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/micgogi/k8s-copilot/cli/internal/anthropic"
)

// Runner is what the agent calls to (a) learn which tools to expose to the
// model and (b) execute a tool the model invoked.
//
// Two implementations: KubectlRunner (real cluster) and the eval FixtureRunner
// (canned JSON outputs). Same interface, so the agent loop is identical for
// both paths.
type Runner interface {
	Tools() []anthropic.Tool
	Run(ctx context.Context, name string, input json.RawMessage) (result string, isError bool)
}

// KubectlRunner shells out to kubectl for each tool call.
type KubectlRunner struct {
	// DefaultNamespace is applied when the model omits the namespace field.
	DefaultNamespace string
}

// NewKubectlRunner builds a KubectlRunner.
func NewKubectlRunner(defaultNamespace string) *KubectlRunner {
	if defaultNamespace == "" {
		defaultNamespace = "default"
	}
	return &KubectlRunner{DefaultNamespace: defaultNamespace}
}

// Tools returns the tool defs published to the model.
func (r *KubectlRunner) Tools() []anthropic.Tool {
	return []anthropic.Tool{
		{
			Name:         "kubectl_get_pods",
			Description:  "List pods in a Kubernetes namespace with their status, ready state, restart counts, and notable conditions. Read-only.",
			InputSchema:  GetPodsSchema(),
			CacheControl: anthropic.Ephemeral(),
		},
	}
}

// Run dispatches a single tool call to its real implementation.
func (r *KubectlRunner) Run(ctx context.Context, name string, raw json.RawMessage) (string, bool) {
	switch name {
	case "kubectl_get_pods":
		var in GetPodsInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return fmt.Sprintf("error parsing tool input: %v", err), true
		}
		if in.Namespace == "" {
			in.Namespace = r.DefaultNamespace
		}
		out, err := GetPods(ctx, in)
		if err != nil {
			return err.Error(), true
		}
		return out, false
	default:
		return fmt.Sprintf("unknown tool: %s", name), true
	}
}
