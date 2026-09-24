package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
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
		res, err := verifyWorkload(client, namespace, resourceType, name)
		if err != nil {
			return err
		}
		truth.ApplyExpectedDigests(&res, expected)
		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			output.PrintJSON(res)
		} else {
			output.PrintTruthResult(res)
		}
		return exitForStatus(res.Status)
	},
}

func verifyWorkload(client kubernetes.Interface, namespace, resourceType, name string) (truth.VerificationResult, error) {
	kind := strings.ToLower(resourceType)
	targetName := name
	if kind == "workload" {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 {
			return truth.VerificationResult{}, fmt.Errorf("verify workload requires <kind>/<name> (e.g. deployment/payments, statefulset/database, daemonset/node-agent)")
		}
		kind = strings.ToLower(parts[0])
		targetName = parts[1]
	}

	switch kind {
	case "deployment":
		dep, err := k8s.GetDeploymentByName(client, namespace, targetName)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		pods, err := k8s.GetDeploymentPods(client, namespace, dep)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		sets, err := k8s.GetReplicaSetsForDeployment(client, namespace, dep)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		return truth.BuildDeploymentResultWithReplicaSets(dep, sets, pods), nil

	case "statefulset":
		sts, err := k8s.GetStatefulSetByName(client, namespace, targetName)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		pods, err := k8s.GetStatefulSetPods(client, namespace, sts)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		revisions, err := k8s.GetControllerRevisionsForStatefulSet(client, namespace, sts)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		return truth.BuildStatefulSetResultWithControllerRevisions(sts, revisions, pods), nil

	case "daemonset":
		ds, err := k8s.GetDaemonSetByName(client, namespace, targetName)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		pods, err := k8s.GetDaemonSetPods(client, namespace, ds)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		revisions, err := k8s.GetControllerRevisionsForDaemonSet(client, namespace, ds)
		if err != nil {
			return truth.VerificationResult{}, err
		}
		return truth.BuildDaemonSetResultWithControllerRevisions(ds, revisions, pods), nil

	default:
		return truth.VerificationResult{}, fmt.Errorf("unsupported resource: %s", resourceType)
	}
}

func init() {
	verifyCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
	verifyCmd.Flags().StringArray("expected-digest", nil, "Expected runtime digest: container=sha256:<digest>; may be repeated")
	verifyCmd.Flags().Bool("json", false, "Emit JSON output")
}
