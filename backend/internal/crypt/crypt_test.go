package crypt

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestAESGCM(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	c, err := NewAESGCM(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := c.Encrypt([]byte("gate code 4471"))
	if bytes.Contains(sealed, []byte("4471")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	plain, err := c.Decrypt(sealed)
	if err != nil || string(plain) != "gate code 4471" {
		t.Fatalf("round trip: %q %v", plain, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := c.Decrypt(sealed); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := NewAESGCM("c2hvcnQ="); err == nil {
		t.Fatal("short key accepted")
	}
	// the built-in development key must be a valid key
	if _, err := NewAESGCM("ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE="); err != nil {
		t.Fatal(err)
	}
}
