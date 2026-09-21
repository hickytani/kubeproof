package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/output"
	"k8s-truth/internal/truth"
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
		if resourceType != "deployment" {
			return fmt.Errorf("unsupported resource for explain: %s", resourceType)
		}
		dep, err := k8s.GetDeploymentByName(client, namespace, name)
		if err != nil {
			return err
		}
		pods, err := k8s.GetDeploymentPods(client, namespace, dep)
		if err != nil {
			return err
		}
		replicaSets, err := k8s.GetReplicaSetsForDeployment(client, namespace, dep)
		if err != nil {
			return err
		}
		res := truth.BuildDeploymentResultWithReplicaSets(dep, replicaSets, pods)
		fmt.Println(output.BuildExplainText(res))
		return nil
	},
}

func init() {
	explainCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
}
