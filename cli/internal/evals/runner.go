package evals

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/micgogi/k8s-copilot/cli/internal/agent"
	"github.com/micgogi/k8s-copilot/cli/internal/anthropic"
)

// Outcome is the result of running and judging a single scenario.
type Outcome struct {
	Scenario    Scenario
	Verdict     Verdict
	AgentText   string
	AgentUsage  anthropic.Usage
	JudgeUsage  anthropic.Usage
	Duration    time.Duration
	Err         error
}

// Approximate prices in dollars per million tokens. Off by ~10% is fine —
// we're tracking the order of magnitude per CLAUDE.md's <$0.10/diagnosis target.
type modelPricing struct {
	in, out, cacheRead, cacheWrite float64
}

var pricing = map[string]modelPricing{
	"claude-sonnet-4-6":          {in: 3.00, out: 15.00, cacheRead: 0.30, cacheWrite: 3.75},
	"claude-haiku-4-5-20251001":  {in: 1.00, out: 5.00, cacheRead: 0.10, cacheWrite: 1.25},
}

func cost(model string, u anthropic.Usage) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	const m = 1_000_000.0
	return (float64(u.InputTokens)*p.in +
		float64(u.OutputTokens)*p.out +
		float64(u.CacheReadInputTokens)*p.cacheRead +
		float64(u.CacheCreationInputTokens)*p.cacheWrite) / m
}

// RunOptions configures a full eval run.
type RunOptions struct {
	// ScenarioDir is the directory of *.json scenario files.
	ScenarioDir string
	// Filter, if non-empty, runs only scenarios whose Name matches exactly.
	Filter string
	// Out is where the human-readable report is written.
	Out io.Writer
}

// Run loads scenarios, executes the agent + judge for each, prints a report,
// and returns the per-scenario outcomes.
func Run(ctx context.Context, opts RunOptions) ([]Outcome, error) {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}

	scenarios, err := LoadScenarios(opts.ScenarioDir)
	if err != nil {
		return nil, err
	}
	if opts.Filter != "" {
		filtered := scenarios[:0]
		for _, s := range scenarios {
			if s.Name == opts.Filter {
				filtered = append(filtered, s)
			}
		}
		scenarios = filtered
		if len(scenarios) == 0 {
			return nil, fmt.Errorf("no scenario named %q in %s", opts.Filter, opts.ScenarioDir)
		}
	}

	client := anthropic.NewClient(apiKey)
	judge := NewJudge(client)

	fmt.Fprintf(opts.Out, "running %d scenario(s) from %s\n\n", len(scenarios), opts.ScenarioDir)

	var (
		outcomes  []Outcome
		totalDur  time.Duration
		totalCost float64
		passes    int
	)

	for _, s := range scenarios {
		o := runOne(ctx, client, judge, s)
		outcomes = append(outcomes, o)
		totalDur += o.Duration
		totalCost += cost(defaultAgentModel(), o.AgentUsage) + cost(judgeModel, o.JudgeUsage)

		switch {
		case o.Err != nil:
			fmt.Fprintf(opts.Out, "  ! %-20s ERROR: %v\n", s.Name, o.Err)
		case o.Verdict.Pass:
			passes++
			fmt.Fprintf(opts.Out, "  ✓ %-20s %5.1fs  agent_tokens=%d/%d  $%.4f\n",
				s.Name, o.Duration.Seconds(),
				o.AgentUsage.InputTokens+o.AgentUsage.CacheReadInputTokens+o.AgentUsage.CacheCreationInputTokens,
				o.AgentUsage.OutputTokens,
				cost(defaultAgentModel(), o.AgentUsage)+cost(judgeModel, o.JudgeUsage),
			)
		default:
			fmt.Fprintf(opts.Out, "  ✗ %-20s %5.1fs  agent_tokens=%d/%d  $%.4f\n",
				s.Name, o.Duration.Seconds(),
				o.AgentUsage.InputTokens+o.AgentUsage.CacheReadInputTokens+o.AgentUsage.CacheCreationInputTokens,
				o.AgentUsage.OutputTokens,
				cost(defaultAgentModel(), o.AgentUsage)+cost(judgeModel, o.JudgeUsage),
			)
			fmt.Fprintf(opts.Out, "    judge: %s\n", o.Verdict.Reason)
		}
	}

	fmt.Fprintf(opts.Out, "\n%d/%d passed (%.1f%%)  total: %.1fs, $%.4f\n",
		passes, len(scenarios),
		100*float64(passes)/float64(max1(len(scenarios))),
		totalDur.Seconds(), totalCost,
	)

	return outcomes, nil
}

func runOne(ctx context.Context, client *anthropic.Client, judge *Judge, s Scenario) Outcome {
	start := time.Now()
	o := Outcome{Scenario: s}

	runner := NewFixtureRunner(s)
	res, err := agent.Run(ctx, client, runner, agent.DiagnoseInput{
		Service: s.Service, Namespace: s.Namespace,
	}, io.Discard)
	if err != nil {
		o.Err = fmt.Errorf("agent: %w", err)
		o.Duration = time.Since(start)
		return o
	}
	o.AgentText = res.FinalText
	o.AgentUsage = res.Usage

	verdict, judgeUsage, err := judge.Score(ctx, s.ExpectedRootCause, res.FinalText)
	o.JudgeUsage = judgeUsage
	if err != nil {
		o.Err = fmt.Errorf("judge: %w", err)
		o.Duration = time.Since(start)
		return o
	}
	o.Verdict = verdict
	o.Duration = time.Since(start)
	return o
}

// defaultAgentModel returns the model the agent loop uses. Centralized so
// cost math stays in sync if the constant in the agent package changes.
func defaultAgentModel() string { return "claude-sonnet-4-6" }

func max1(n int) int {
	if n == 0 {
		return 1
	}
	return n
}
