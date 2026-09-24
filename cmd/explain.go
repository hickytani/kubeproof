package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/output"
)

var explainCmd = &cobra.Command{
	Use:   "explain <resource> <name>",
	Short: "Explain a verification result from existing evidence",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceType := strings.ToLower(args[0])
		name := args[1]
		namespace, _ := cmd.Flags().GetString("namespace")
		if namespace == "" {
			namespace = "default"
		}
		contextName, _ := cmd.Root().Flags().GetString("context")
		client, err := k8s.NewClientWithContext(contextName)
		if err != nil {
			return err
		}
		res, err := verifyWorkload(client, namespace, resourceType, name)
		if err != nil {
			return err
		}
		fmt.Println(output.BuildExplainText(res))
		return exitForStatus(res.Status)
	},
}

func init() {
	explainCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
}
