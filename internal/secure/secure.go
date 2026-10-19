// Package secure holds small crypto helpers: token generation, hashing, and
// symmetric encryption of provider credentials at rest.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// RandomHex returns n random bytes, hex encoded.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// HashToken hashes a high-entropy token (API key, session token) for storage.
// A fast hash is fine here because the input is random, not a password.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Mask shows only the edges of a secret, e.g. "sk-…abcd".
func Mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "…" + s[len(s)-min(len(s), 2):]
	}
	head := 3
	if i := strings.IndexByte(s, '-'); i > 0 && i < 8 {
		head = i + 1
	}
	return s[:head] + "…" + s[len(s)-4:]
}

// Box encrypts and decrypts short strings with AES-256-GCM.
type Box struct{ aead cipher.AEAD }

func NewBox(secret string) (*Box, error) {
	key := sha256.Sum256([]byte("plexrouter:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plain string) string {
	if plain == "" {
		return ""
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(out)
}

func (b *Box) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errors.New("cannot decrypt provider key (was PLEXROUTER_SECRET changed?)")
	}
	return string(plain), nil
}
