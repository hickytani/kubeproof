package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/output"
	"k8s-truth/internal/truth"
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
		if resourceType != "deployment" {
			return fmt.Errorf("unsupported resource for evidence: %s", resourceType)
		}
		dep, err := k8s.GetDeploymentByName(client, namespace, name)
		if err != nil {
			return err
		}
		pods, err := k8s.GetDeploymentPods(client, namespace, dep)
		if err != nil {
			return err
		}
		res := truth.BuildDeploymentResult(dep, pods)
		if jsonOut {
			output.PrintJSON(res)
			return nil
		}
		output.PrintTruthResult(res)
		return nil
	},
}

func init() {
	evidenceCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
	evidenceCmd.Flags().Bool("json", false, "Emit JSON output")
}
