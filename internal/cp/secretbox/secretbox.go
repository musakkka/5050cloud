// SPDX-License-Identifier: AGPL-3.0-only

// Package secretbox encrypts secret columns in state.db with the install's
// master key (XChaCha20-Poly1305, random 24-byte nonce prefixed to the
// ciphertext). The additional data binds a ciphertext to its row so values
// cannot be swapped between rows. Shared by core (WP1) and orch (WP2);
// owned by the planner. See docs/SPEC.md 6 and 15.
package secretbox

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/chacha20poly1305"
)

// KeySize is the master key length in bytes.
const KeySize = chacha20poly1305.KeySize

// Box seals and opens values with one key.
type Box struct{ key []byte }

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secretbox: key must be %d bytes, got %d", KeySize, len(key))
	}
	k := make([]byte, KeySize)
	copy(k, key)
	return &Box{key: k}, nil
}

// LoadFile reads a raw 32-byte key file (for example /etc/5050/master.key).
func LoadFile(path string) (*Box, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return New(b)
}

// GenerateKey returns a new random key.
func GenerateKey() ([]byte, error) {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, nil
}

// Seal encrypts plaintext bound to aad (for example "variables|env_x|svc_y|NAME").
func (b *Box) Seal(plaintext []byte, aad string) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(b.key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plaintext)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, []byte(aad)), nil
}

// ErrOpen is returned for a wrong key, wrong aad or tampered ciphertext.
var ErrOpen = errors.New("secretbox: cannot decrypt")

// Open decrypts a value produced by Seal with the same aad.
func (b *Box) Open(ciphertext []byte, aad string) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(b.key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrOpen
	}
	nonce, ct := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, ct, []byte(aad))
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}
