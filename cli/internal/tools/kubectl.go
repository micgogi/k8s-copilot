// Package tools holds read-only tools the agent can call.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// GetPodsInput is the JSON input the LLM passes to the kubectl_get_pods tool.
type GetPodsInput struct {
	Namespace string `json:"namespace"`
}

// PodSummary is a compact pod view tuned for LLM consumption.
type PodSummary struct {
	Name       string   `json:"name"`
	Phase      string   `json:"phase"`
	Ready      string   `json:"ready"`
	Restarts   int      `json:"restarts"`
	StartedAt  string   `json:"started_at,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
}

// GetPodsSchema is the JSON Schema published to the model for kubectl_get_pods.
func GetPodsSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "namespace": {
      "type": "string",
      "description": "Kubernetes namespace to list pods from. Defaults to 'default' if empty."
    }
  },
  "required": []
}`)
}

// GetPods shells out to `kubectl get pods -n <ns> -o json` and returns a
// compact JSON summary. Read-only.
func GetPods(ctx context.Context, in GetPodsInput) (string, error) {
	ns := in.Namespace
	if ns == "" {
		ns = "default"
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", ns, "-o", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get pods -n %s: %w (output: %s)", ns, err, string(out))
	}

	var raw struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				StartTime         string `json:"startTime"`
				ContainerStatuses []struct {
					Ready        bool `json:"ready"`
					RestartCount int  `json:"restartCount"`
				} `json:"containerStatuses"`
				Conditions []struct {
					Type    string `json:"type"`
					Status  string `json:"status"`
					Reason  string `json:"reason,omitempty"`
					Message string `json:"message,omitempty"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return "", fmt.Errorf("parse kubectl json: %w", err)
	}

	pods := make([]PodSummary, 0, len(raw.Items))
	for _, p := range raw.Items {
		ready := 0
		restarts := 0
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
			restarts += cs.RestartCount
		}
		var notable []string
		for _, c := range p.Status.Conditions {
			if c.Status != "True" {
				msg := c.Type + "=" + c.Status
				if c.Reason != "" {
					msg += " (" + c.Reason + ")"
				}
				if c.Message != "" {
					msg += ": " + c.Message
				}
				notable = append(notable, msg)
			}
		}
		pods = append(pods, PodSummary{
			Name:       p.Metadata.Name,
			Phase:      p.Status.Phase,
			Ready:      fmt.Sprintf("%d/%d", ready, len(p.Status.ContainerStatuses)),
			Restarts:   restarts,
			StartedAt:  p.Status.StartTime,
			Conditions: notable,
		})
	}

	summary, err := json.MarshalIndent(map[string]any{
		"namespace": ns,
		"pod_count": len(pods),
		"pods":      pods,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal summary: %w", err)
	}
	return string(summary), nil
}
