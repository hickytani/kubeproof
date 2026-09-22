package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s-truth/internal/attest"
)

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate an Ed25519 signing key pair",
	Long:  "Generate an Ed25519 private/public key pair for signing and verifying runtime attestations.",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			return fmt.Errorf("--output is required")
		}

		publicKeyPath, _ := cmd.Flags().GetString("public-key")
		if publicKeyPath == "" {
			publicKeyPath = output + ".pub"
		}

		pub, priv, err := attest.GenerateKeyPair()
		if err != nil {
			return fmt.Errorf("generate key pair: %w", err)
		}

		if err := attest.WritePrivateKey(output, priv); err != nil {
			return err
		}

		if err := attest.WritePublicKey(publicKeyPath, pub); err != nil {
			return err
		}

		fmt.Printf("Private key: %s\n", output)
		fmt.Printf("Public key:  %s\n", publicKeyPath)
		fmt.Printf("Fingerprint: %s\n", attest.PublicKeyFingerprint(pub))
		return nil
	},
}

func init() {
	keygenCmd.Flags().String("output", "", "Path for the private key file (required)")
	keygenCmd.Flags().String("public-key", "", "Path for the public key file (defaults to <output>.pub)")
}
