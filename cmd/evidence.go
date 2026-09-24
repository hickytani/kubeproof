package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/output"
)

var evidenceCmd = &cobra.Command{
	Use:   "evidence <resource> <name>",
	Short: "Emit JSON evidence for a verification result",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceType := strings.ToLower(args[0])
		name := args[1]
		namespace, _ := cmd.Flags().GetString("namespace")
		if namespace == "" {
			namespace = "default"
		}
		jsonOut, _ := cmd.Flags().GetBool("json")
		contextName, _ := cmd.Root().Flags().GetString("context")
		client, err := k8s.NewClientWithContext(contextName)
		if err != nil {
			return err
		}
		res, err := verifyWorkload(client, namespace, resourceType, name)
		if err != nil {
			return err
		}
		if jsonOut {
			output.PrintJSON(res)
			return exitForStatus(res.Status)
		}
		output.PrintTruthResult(res)
		return exitForStatus(res.Status)
	},
}

func init() {
	evidenceCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
	evidenceCmd.Flags().Bool("json", false, "Emit JSON output")
}
