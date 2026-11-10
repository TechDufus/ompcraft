package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrNoReleaseKey is returned when the build embeds no release public key.
var ErrNoReleaseKey = errors.New("this build has no release key; updates are disabled")

func decodeKey(b64 string, size int, what string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("the %s is not valid base64", what)
	}
	if len(raw) != size {
		return nil, fmt.Errorf("the %s must be %d bytes, got %d", what, size, len(raw))
	}
	return raw, nil
}

// VerifySignature checks that sigB64 (base64 of an ed25519 signature) is a
// valid signature of msg by the key pubB64 (base64 of a 32-byte public key).
func VerifySignature(pubB64 string, msg, sigB64 []byte) error {
	if strings.TrimSpace(pubB64) == "" {
		return ErrNoReleaseKey
	}
	pub, err := decodeKey(pubB64, ed25519.PublicKeySize, "release public key")
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("checksums.txt.sig is not a base64 ed25519 signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
		return errors.New("signature verification failed: checksums.txt was not signed by the release key")
	}
	return nil
}

// GenerateKey returns a new key pair: the base64 public key (32 bytes) and the
// base64 private key (64 bytes).
func GenerateKey() (pubB64, privB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

// Sign returns base64(ed25519.Sign(priv, data)) for a base64 64-byte private
// key. Errors never contain key material.
func Sign(privB64 string, data []byte) (string, error) {
	raw, err := decodeKey(privB64, ed25519.PrivateKeySize, "signing key")
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(raw), data)), nil
}
