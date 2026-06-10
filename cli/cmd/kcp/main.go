package main

import (
	"fmt"
	"os"

	"github.com/micgogi/k8s-copilot/cli/internal/agent"
	"github.com/micgogi/k8s-copilot/cli/internal/evals"
	"github.com/spf13/cobra"
)

var version = "0.0.1-dev"

func main() {
	root := &cobra.Command{
		Use:           "kcp",
		Short:         "k8s-copilot: an LLM-powered Kubernetes debugging assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(versionCmd(), diagnoseCmd(), evalCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("kcp %s\n", version)
		},
	}
}

func diagnoseCmd() *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "diagnose [service]",
		Short: "Diagnose a misbehaving service",
		Long: `Diagnose a misbehaving service in your Kubernetes cluster.

The copilot pulls live state from the cluster and uses an LLM agent to identify
likely root causes with cited evidence.

If no service is provided, kcp scans the entire namespace.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var service string
			if len(args) > 0 {
				service = args[0]
			}
			return agent.Diagnose(cmd.Context(), agent.DiagnoseInput{
				Service:   service,
				Namespace: namespace,
			})
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	return cmd
}

func evalCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "eval [scenario]",
		Short: "Run the agent against golden scenarios and score with an LLM judge",
		Long: `Run the agent against hand-authored scenarios under evals/scenarios/.

Each scenario provides canned tool outputs and a ground-truth root cause. The
agent runs against the canned outputs (no cluster needed); a Haiku judge scores
its diagnosis against the ground truth.

If a scenario name is provided, only that scenario runs.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var filter string
			if len(args) > 0 {
				filter = args[0]
			}
			outcomes, err := evals.Run(cmd.Context(), evals.RunOptions{
				ScenarioDir: dir,
				Filter:      filter,
				Out:         os.Stdout,
			})
			if err != nil {
				return err
			}
			for _, o := range outcomes {
				if o.Err == nil && !o.Verdict.Pass {
					os.Exit(1)
				}
				if o.Err != nil {
					os.Exit(2)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "evals/scenarios", "Directory of scenario JSON files")
	return cmd
}
