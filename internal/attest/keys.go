package attest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
)

// GenerateKeyPair creates a new Ed25519 key pair using crypto/rand.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// WritePrivateKey writes an Ed25519 private key to a file in PEM-encoded PKCS8 format.
func WritePrivateKey(path string, key ed25519.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0600)
}

// WritePublicKey writes an Ed25519 public key to a file in PEM-encoded PKIX format.
func WritePublicKey(path string, key ed25519.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0644)
}

// ReadPrivateKey reads a PEM-encoded PKCS8 Ed25519 private key from a file.
func ReadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("unexpected PEM type %q, expected PRIVATE KEY", block.Type)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	ed, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not Ed25519")
	}
	return ed, nil
}

// ReadPublicKey reads a PEM-encoded PKIX Ed25519 public key from a file.
func ReadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("unexpected PEM type %q, expected PUBLIC KEY", block.Type)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	ed, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not Ed25519")
	}
	return ed, nil
}

// PublicKeyFingerprint returns a deterministic SHA-256 fingerprint of an Ed25519 public key.
func PublicKeyFingerprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return "sha256:" + hex.EncodeToString(h[:])
}
