package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s-truth/internal/attest"
)

var verifyAttestationCmd = &cobra.Command{
	Use:   "verify-attestation <attestation-file>",
	Short: "Verify a signed attestation offline",
	Long:  "Verify the cryptographic signature and evidence integrity of a signed attestation without Kubernetes access.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		attestationPath := args[0]
		publicKeyPath, _ := cmd.Flags().GetString("public-key")
		if publicKeyPath == "" {
			return fmt.Errorf("--public-key is required")
		}

		// Read the attestation file.
		data, err := os.ReadFile(attestationPath)
		if err != nil {
			return fmt.Errorf("read attestation: %w", err)
		}

		var attestation attest.Attestation
		if err := json.Unmarshal(data, &attestation); err != nil {
			return fmt.Errorf("parse attestation: %w", err)
		}

		// Read the public key.
		publicKey, err := attest.ReadPublicKey(publicKeyPath)
		if err != nil {
			return fmt.Errorf("read public key: %w", err)
		}

		// Verify the attestation offline.
		report, err := attest.VerifyAttestation(&attestation, publicKey)
		if err != nil {
			return fmt.Errorf("verify attestation: %w", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(report)
		} else {
			printVerificationReport(report)
		}

		if !report.Valid {
			return verificationExitError{2}
		}
		return nil
	},
}

func printVerificationReport(report *attest.VerificationReport) {
	fmt.Println("KubeProof Attestation Verification")
	fmt.Println("────────────────────────────────────────")

	if report.Valid {
		fmt.Println("Status:      VERIFIED")
	} else {
		fmt.Println("Status:      INVALID")
	}

	if report.SignatureValid {
		fmt.Println("Signature:   VALID")
	} else {
		fmt.Println("Signature:   INVALID")
	}

	if report.Attestation != nil {
		a := report.Attestation
		fmt.Printf("Workload:    %s\n", a.Workload)
		fmt.Printf("Namespace:   %s\n", a.Namespace)
		fmt.Printf("Revision:    %s\n", a.DeploymentRevision)
		fmt.Printf("Created:     %s\n", a.CreatedAt)
		fmt.Printf("Signer:      %s\n", a.Signer.PublicKeyFingerprint)
		fmt.Println()

		// Runtime identity evidence.
		if len(a.ExpectedDigests) > 0 || len(a.ObservedContainerEvidence) > 0 {
			fmt.Println("Runtime identity:")
			seen := map[string]bool{}
			for _, ed := range a.ExpectedDigests {
				for _, oc := range a.ObservedContainerEvidence {
					if oc.ContainerName == ed.Container {
						fmt.Printf("  %s\n", ed.Container)
						fmt.Printf("    expected: %s\n", ed.Digest)
						observed := extractDigestFromImageRef(oc.RuntimeImageID)
						fmt.Printf("    observed: %s\n", observed)
						fmt.Printf("    result:   %s\n", oc.VerificationResult)
						seen[ed.Container] = true
						break
					}
				}
			}
			for _, oc := range a.ObservedContainerEvidence {
				if !seen[oc.ContainerName] {
					fmt.Printf("  %s\n", oc.ContainerName)
					fmt.Printf("    declared: %s\n", oc.DeclaredImage)
					fmt.Printf("    observed: %s\n", oc.RuntimeImageID)
					fmt.Printf("    result:   %s\n", oc.VerificationResult)
				}
			}
			fmt.Println()
		}

		fmt.Printf("Evidence:\n")
		podCount := map[string]bool{}
		for _, oc := range a.ObservedContainerEvidence {
			podCount[oc.PodName] = true
		}
		fmt.Printf("  Pods verified:       %d\n", len(podCount))
		fmt.Printf("  Containers verified: %d\n", len(a.ObservedContainerEvidence))
		fmt.Printf("  Replicas:            %d desired, %d observed, %d matching\n",
			a.ReplicaEvidence.Desired, a.ReplicaEvidence.Observed, a.ReplicaEvidence.Matching)
		fmt.Println()
	}

	if report.TimestampPresent && report.Attestation != nil {
		if ts, err := time.Parse(time.RFC3339, report.Attestation.CreatedAt); err == nil {
			fmt.Printf("Observation age: %s\n", time.Since(ts).Round(time.Second))
		}
	}

	if report.Valid {
		fmt.Println("\nAttestation: VALID")
	} else {
		fmt.Println("\nAttestation: INVALID")
		for _, e := range report.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}
}

func extractDigestFromImageRef(ref string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == '@' {
			return ref[i+1:]
		}
	}
	return ref
}

func init() {
	verifyAttestationCmd.Flags().String("public-key", "", "Path to Ed25519 public key file (required)")
	verifyAttestationCmd.Flags().Bool("json", false, "Emit JSON output")
}
