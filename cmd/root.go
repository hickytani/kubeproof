package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s-truth/internal/truth"
)

var rootCmd = &cobra.Command{
	Use:           "stateproof",
	Short:         "Read-only Kubernetes workload truth verifier",
	Long:          "stateproof inspects Kubernetes workload identity and reports evidence-backed mismatches without mutating the cluster.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

type verificationExitError struct{ code int }

func (e verificationExitError) Error() string { return "" }
func exitForStatus(status truth.Status) error {
	switch status {
	case truth.StatusMatch:
		return nil
	case truth.StatusPartial, truth.StatusMismatch, truth.StatusStale:
		return verificationExitError{2}
	default:
		return verificationExitError{3}
	}
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
		var verification verificationExitError
		if errors.As(err, &verification) {
			os.Exit(verification.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
