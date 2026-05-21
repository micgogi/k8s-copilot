package main

import (
	"fmt"
	"os"

	"github.com/micgogi/k8s-copilot/cli/internal/agent"
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
	root.AddCommand(versionCmd(), diagnoseCmd())

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
