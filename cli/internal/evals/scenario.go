// Package evals runs the agent against hand-authored scenarios and scores it
// with an LLM judge. Scenarios are JSON files under evals/scenarios/.
package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/micgogi/k8s-copilot/cli/internal/anthropic"
	"github.com/micgogi/k8s-copilot/cli/internal/tools"
)

// Scenario is one golden incident loaded from JSON. See evals/scenarios/*.json.
type Scenario struct {
	// Name identifies the scenario in eval output. Filled from filename if absent.
	Name string `json:"name"`

	// Service is optional — passed to the agent as the focused service.
	Service string `json:"service,omitempty"`

	// Namespace passed to the agent. Defaults to "default".
	Namespace string `json:"namespace,omitempty"`

	// Fixtures maps tool name → canned JSON output the FixtureRunner returns
	// when the model invokes that tool. The raw string is sent to the model
	// verbatim as the tool_result content.
	Fixtures map[string]json.RawMessage `json:"fixtures"`

	// ExpectedRootCause is the truth the LLM judge checks the agent's answer
	// against. Plain English; the judge handles wording variation.
	ExpectedRootCause string `json:"expected_root_cause"`
}

// LoadScenarios reads every *.json under dir as a Scenario, sorted by name.
func LoadScenarios(dir string) ([]Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read scenarios dir %s: %w", dir, err)
	}
	var out []Scenario
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, err := loadScenario(path)
		if err != nil {
			return nil, err
		}
		if s.Name == "" {
			s.Name = strings.TrimSuffix(e.Name(), ".json")
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadScenario(path string) (Scenario, error) {
	var s Scenario
	body, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return s, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.ExpectedRootCause == "" {
		return s, fmt.Errorf("%s: expected_root_cause is required", path)
	}
	return s, nil
}

// FixtureRunner satisfies tools.Runner from a scenario's canned outputs.
// If the model invokes a tool the scenario doesn't define a fixture for,
// the runner returns an is_error result rather than crashing — that itself
// is useful signal (the model went off-piste).
type FixtureRunner struct {
	toolDefs []anthropic.Tool
	fixtures map[string]json.RawMessage
}

// NewFixtureRunner builds a FixtureRunner that exposes the same tool defs
// as the live runner (so the model sees the same surface area) but returns
// fixture data instead of shelling out.
func NewFixtureRunner(s Scenario) *FixtureRunner {
	live := tools.NewKubectlRunner(s.Namespace)
	return &FixtureRunner{
		toolDefs: live.Tools(),
		fixtures: s.Fixtures,
	}
}

// Tools returns the publicly advertised tool defs.
func (f *FixtureRunner) Tools() []anthropic.Tool { return f.toolDefs }

// Run returns the canned output for the named tool, or an error result if
// the scenario didn't provide a fixture.
func (f *FixtureRunner) Run(_ context.Context, name string, _ json.RawMessage) (string, bool) {
	raw, ok := f.fixtures[name]
	if !ok {
		return fmt.Sprintf("no fixture provided for tool %q in this scenario", name), true
	}
	return string(raw), false
}
