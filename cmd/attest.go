package cmd

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s-truth/internal/attest"
	"k8s-truth/internal/k8s"
	"k8s-truth/internal/truth"
)

var attestCmd = &cobra.Command{
	Use:   "attest <resource> <name>",
	Short: "Create a signed attestation from a successful runtime verification",
	Long:  "Verify a workload and produce a cryptographically signed attestation artifact if verification succeeds.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceType := strings.ToLower(args[0])
		name := args[1]

		if resourceType != "workload" {
			return fmt.Errorf("attest supports 'workload' resource type only; got %q", resourceType)
		}

		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "deployment" {
			return fmt.Errorf("attest workload supports deployment/<name> only")
		}
		deploymentName := parts[1]

		namespace, _ := cmd.Flags().GetString("namespace")
		if namespace == "" {
			namespace = "default"
		}

		signingKeyPath, _ := cmd.Flags().GetString("signing-key")
		if signingKeyPath == "" {
			return fmt.Errorf("--signing-key is required")
		}

		outputPath, _ := cmd.Flags().GetString("output")
		if outputPath == "" {
			return fmt.Errorf("--output is required")
		}

		expectedValues, _ := cmd.Flags().GetStringArray("expected-digest")
		expected, err := truth.ParseExpectedDigests(expectedValues)
		if err != nil {
			return err
		}

		// Read the signing key.
		privateKey, err := attest.ReadPrivateKey(signingKeyPath)
		if err != nil {
			return fmt.Errorf("read signing key: %w", err)
		}
		publicKey := privateKey.Public().(ed25519.PublicKey)

		// Create Kubernetes client and perform verification.
		contextName, _ := cmd.Root().Flags().GetString("context")
		client, err := k8s.NewClientWithContext(contextName)
		if err != nil {
			return err
		}

		dep, err := k8s.GetDeploymentByName(client, namespace, deploymentName)
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

		// Refuse to attest if verification is not MATCH.
		if res.Status != truth.StatusMatch {
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				enc := json.NewEncoder(os.Stderr)
				enc.SetIndent("", "  ")
				enc.Encode(res)
			}
			fmt.Fprintf(os.Stderr, "Attestation refused: verification result is %s, not MATCH\n", res.Status)
			return verificationExitError{2}
		}

		// Create and sign the attestation.
		attestation, err := attest.NewAttestation(res, expected, publicKey)
		if err != nil {
			return fmt.Errorf("create attestation: %w", err)
		}

		if err := attest.SignAttestation(attestation, privateKey); err != nil {
			return fmt.Errorf("sign attestation: %w", err)
		}

		// Write attestation to file.
		data, err := json.MarshalIndent(attestation, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal attestation: %w", err)
		}

		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return fmt.Errorf("write attestation: %w", err)
		}

		fmt.Printf("Attestation written to %s\n", outputPath)
		fmt.Printf("Workload:    %s\n", attestation.Workload)
		fmt.Printf("Namespace:   %s\n", attestation.Namespace)
		fmt.Printf("Revision:    %s\n", attestation.DeploymentRevision)
		fmt.Printf("Result:      %s\n", attestation.VerificationResult)
		fmt.Printf("Signer:      %s\n", attestation.Signer.PublicKeyFingerprint)
		fmt.Printf("Created:     %s\n", attestation.CreatedAt)
		return nil
	},
}

func init() {
	attestCmd.Flags().StringP("namespace", "n", "default", "Namespace to inspect")
	attestCmd.Flags().StringArray("expected-digest", nil, "Expected runtime digest: container=sha256:<digest>; may be repeated")
	attestCmd.Flags().String("signing-key", "", "Path to Ed25519 private key file (required)")
	attestCmd.Flags().String("output", "", "Path for the attestation output file (required)")
	attestCmd.Flags().Bool("json", false, "Emit JSON output on error")
}
