package cmd

import (
	"github.com/spf13/cobra"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/output"
	"k8s-truth/internal/truth"
)

var scanCmd = &cobra.Command{
	Use:   "scan <namespace>",
	Short: "Scan a namespace for deployment drift",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		namespace := args[0]
		contextName, _ := cmd.Root().Flags().GetString("context")
		client, err := k8s.NewClientWithContext(contextName)
		if err != nil {
			return err
		}
		deployments, err := k8s.GetNamespaceDeployments(client, namespace)
		if err != nil {
			return err
		}
		results := map[string]truth.VerificationResult{}
		for _, dep := range deployments {
			pods, err := k8s.GetDeploymentPods(client, namespace, &dep)
			if err != nil {
				results[dep.Name] = truth.VerificationResult{Claim: "deployment verify", Subject: dep.Name, Status: truth.StatusUnknown, Source: "Kubernetes API", Limitations: []string{err.Error()}}
				continue
			}
			replicaSets, err := k8s.GetReplicaSetsForDeployment(client, namespace, &dep)
			if err != nil {
				results[dep.Name] = truth.VerificationResult{Claim: "deployment verify", Subject: dep.Name, Status: truth.StatusUnknown, Source: "Kubernetes API", Limitations: []string{err.Error()}}
				continue
			}
			results[dep.Name] = truth.BuildDeploymentResultWithReplicaSets(&dep, replicaSets, pods)
		}
		out := truth.BuildScanSummary(deployments, results)
		output.PrintScanSummary(out)
		return nil
	},
}
