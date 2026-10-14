// Package crypt encrypts sensitive columns (the *_enc BYTEA fields) before they reach Postgres.
// The Cipher interface is the seam for AWS KMS envelope encryption in production; AESGCM is the
// local implementation used in development, tests and as the data-key wrapper.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

type Cipher interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(sealed []byte) ([]byte, error)
}

type AESGCM struct{ aead cipher.AEAD }

// NewAESGCM takes a base64-encoded 32-byte key.
func NewAESGCM(b64Key string) (*AESGCM, error) {
	key, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil || len(key) != 32 {
		return nil, errors.New("FIELD_ENCRYPTION_KEY must be 32 bytes, base64 encoded")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

func (c *AESGCM) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}

func (c *AESGCM) Decrypt(sealed []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return c.aead.Open(nil, sealed[:n], sealed[n:], nil)
}

// Keyed hashing for one-time codes, door codes and session tokens' lookup keys.
func HMAC(key []byte, parts ...string) []byte {
	m := hmac.New(sha256.New, key)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{0})
	}
	return m.Sum(nil)
}

func SHA256(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// Equal compares two MACs in constant time.
func Equal(a, b []byte) bool { return hmac.Equal(a, b) }
