package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/output"
	"k8s-truth/internal/truth"
)

var verifyCmd = &cobra.Command{
	Use:   "verify <resource> <name>",
	Short: "Verify a workload against its declared state",
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
		switch resourceType {
		case "deployment":
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
			output.PrintTruthResult(res)
			return exitForStatus(res.Status)
		case "workload":
			return fmt.Errorf("verify workload is not supported: StateProof currently verifies Deployments only; use verify deployment %s", name)
		default:
			return fmt.Errorf("unsupported resource: %s", resourceType)
		}
	},
}

func init() {
	verifyCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
}
