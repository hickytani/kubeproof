package output

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"k8s-truth/internal/truth"
)

func PrintTruthResult(res truth.VerificationResult) {
	fmt.Println("TRUTH")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("Subject: %s\n", res.Subject)
	fmt.Printf("Status: %s\n", res.Status)
	fmt.Printf("Desired: %s\n", res.Desired)
	if res.Observed != "" {
		fmt.Printf("Observed: %s\n", res.Observed)
	}
	fmt.Printf("Source: %s\n", res.Source)
	fmt.Printf("Timestamp: %s\n", res.Timestamp.Format(time.RFC3339))
	if len(res.Limitations) > 0 {
		fmt.Println("LIMITS")
		for _, lim := range res.Limitations {
			fmt.Printf("  ? %s\n", lim)
		}
	}
	if len(res.Observations) > 0 {
		fmt.Println("OBSERVATIONS")
		for _, obs := range res.Observations {
			fmt.Printf("  - %s %s -> %s (%s)\n", obs.Kind, obs.Subject, obs.Value, obs.Status)
		}
	}
}

func PrintScanSummary(summary string) {
	fmt.Println("TRUTH SCAN")
	fmt.Println("────────────────────────────────────────")
	fmt.Println(summary)
}

func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal JSON: %v\n", err)
		os.Exit(1)
	}
}

func BuildExplainText(res truth.VerificationResult) string {
	lines := []string{
		"WHY RESULT?",
		"----------",
		fmt.Sprintf("Claim: %s", res.Claim),
		fmt.Sprintf("Status: %s", res.Status),
		fmt.Sprintf("Desired: %s", res.Desired),
		fmt.Sprintf("Observed: %s", res.Observed),
	}
	if len(res.Observations) > 0 {
		lines = append(lines, "")
		lines = append(lines, "EVIDENCE")
		for _, obs := range res.Observations {
			lines = append(lines, fmt.Sprintf("  - %s %s => %s [%s]", obs.Kind, obs.Subject, obs.Value, obs.Status))
		}
	}
	if len(res.Limitations) > 0 {
		lines = append(lines, "")
		lines = append(lines, "LIMITS")
		for _, lim := range res.Limitations {
			lines = append(lines, fmt.Sprintf("  ? %s", lim))
		}
	}
	return strings.Join(lines, "\n")
}

func SortStrings(items []string) []string {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	return sorted
}
