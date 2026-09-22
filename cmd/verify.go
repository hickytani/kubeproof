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
		expectedValues, _ := cmd.Flags().GetStringArray("expected-digest")
		expected, err := truth.ParseExpectedDigests(expectedValues)
		if err != nil {
			return err
		}
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
			truth.ApplyExpectedDigests(&res, expected)
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				output.PrintJSON(res)
			} else {
				output.PrintTruthResult(res)
			}
			return exitForStatus(res.Status)
		case "workload":
			parts := strings.SplitN(name, "/", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "deployment" {
				return fmt.Errorf("verify workload supports deployment/<name> only")
			}
			dep, err := k8s.GetDeploymentByName(client, namespace, parts[1])
			if err != nil {
				return err
			}
			pods, err := k8s.GetDeploymentPods(client, namespace, dep)
			if err != nil {
				return err
			}
			sets, err := k8s.GetReplicaSetsForDeployment(client, namespace, dep)
			if err != nil {
				return err
			}
			res := truth.BuildDeploymentResultWithReplicaSets(dep, sets, pods)
			truth.ApplyExpectedDigests(&res, expected)
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				output.PrintJSON(res)
			} else {
				output.PrintTruthResult(res)
			}
			return exitForStatus(res.Status)
		default:
			return fmt.Errorf("unsupported resource: %s", resourceType)
		}
	},
}

func init() {
	verifyCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
	verifyCmd.Flags().StringArray("expected-digest", nil, "Expected runtime digest: container=sha256:<digest>; may be repeated")
	verifyCmd.Flags().Bool("json", false, "Emit JSON output")
}
