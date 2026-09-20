package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "stateproof",
	Short:        "Read-only Kubernetes workload truth verifier",
	Long:         "stateproof inspects Kubernetes workload identity and reports evidence-backed mismatches without mutating the cluster.",
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().String("context", "", "Kubernetes context to use (defaults to current kubeconfig context)")
	rootCmd.AddCommand(verifyCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(explainCmd)
	rootCmd.AddCommand(evidenceCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
