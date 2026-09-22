package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"k8s-truth/internal/output"
	"k8s-truth/internal/truth"
	"os"
)

var compareCmd = &cobra.Command{Use: "compare <before.json> <after.json>", Short: "Compare two offline evidence snapshots", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	read := func(path string) (truth.VerificationResult, error) {
		f, e := os.Open(path)
		if e != nil {
			return truth.VerificationResult{}, e
		}
		defer f.Close()
		var s truth.VerificationResult
		e = json.NewDecoder(f).Decode(&s)
		return s, e
	}
	b, e := read(args[0])
	if e != nil {
		return fmt.Errorf("read before snapshot: %w", e)
	}
	a, e := read(args[1])
	if e != nil {
		return fmt.Errorf("read after snapshot: %w", e)
	}
	r, e := truth.CompareSnapshots(b, a)
	if e != nil {
		return e
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		output.PrintJSON(r)
	} else {
		fmt.Printf("KubeProof Evidence Comparison\nWorkload: %s\nResult: %s\n", r.Workload, r.Status)
		for _, c := range r.Changes {
			fmt.Printf("- %s %s: %s -> %s\n", c.Code, c.Subject, c.Before, c.After)
		}
	}
	if r.Status == truth.ComparisonChanged {
		return verificationExitError{2}
	}
	return nil
}}

func init() {
	compareCmd.Flags().Bool("json", false, "Emit JSON output")
	rootCmd.AddCommand(compareCmd)
}
