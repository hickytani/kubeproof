package attest

import (
	"crypto/ed25519"
)

// Sign signs a payload using an Ed25519 private key.
func Sign(payload []byte, privateKey ed25519.PrivateKey) []byte {
	return ed25519.Sign(privateKey, payload)
}

// Verify checks an Ed25519 signature against a payload and public key.
func Verify(payload []byte, signature []byte, publicKey ed25519.PublicKey) bool {
	return ed25519.Verify(publicKey, payload, signature)
}
