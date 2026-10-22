// SPDX-License-Identifier: AGPL-3.0-only

// Package minisign signs and verifies files in the minisign format
// (https://jedisct1.github.io/minisign/). Releases sign checksums.txt with
// it; install/install.sh and internal/upgrade verify the signature before
// they trust any checksum (docs/SPEC.md 4.2 step 6 and 9.16).
//
// Verification accepts both signature algorithms minisign writes: "Ed"
// (Ed25519 over the file) and "ED" (Ed25519 over the BLAKE2b-512 hash of the
// file, the default of current minisign releases). Signing always writes
// "ED". Secret keys are read and written unencrypted (the format of
// `minisign -G -W`); keep them in a secret store, never on a host.
package minisign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const (
	algLegacy   = "Ed" // signature over the raw message
	algHashed   = "ED" // signature over BLAKE2b-512(message)
	pubKeyLen   = 2 + 8 + ed25519.PublicKeySize
	sigLen      = 2 + 8 + ed25519.SignatureSize
	secKeyLen   = 2 + 2 + 2 + 32 + 8 + 8 + 8 + ed25519.PrivateKeySize + 32
	trustedPfx  = "trusted comment: "
	untrustPfx  = "untrusted comment: "
	maxSigBytes = 64 << 10
)

// KeyID identifies a key pair. minisign prints it as 16 hex digits of the
// little-endian number.
type KeyID [8]byte

func (k KeyID) String() string {
	return strings.ToUpper(fmt.Sprintf("%016x", binary.LittleEndian.Uint64(k[:])))
}

// PublicKey is a minisign public key.
type PublicKey struct {
	ID  KeyID
	Key ed25519.PublicKey
}

// PrivateKey is an unencrypted minisign secret key.
type PrivateKey struct {
	ID  KeyID
	Key ed25519.PrivateKey
}

// Signature is a parsed .minisig file.
type Signature struct {
	Algorithm        string
	KeyID            KeyID
	Signature        []byte
	UntrustedComment string
	TrustedComment   string
	GlobalSignature  []byte
}

// ParsePublicKey reads a public key from either the one-line base64 form
// (what `minisign -P` takes) or a two-line .pub file.
func ParsePublicKey(s string) (PublicKey, error) {
	line := strings.TrimSpace(s)
	if strings.HasPrefix(line, untrustPfx) {
		parts := strings.SplitN(line, "\n", 2)
		if len(parts) != 2 {
			return PublicKey{}, errors.New("the public key file has a comment line but no key line")
		}
		line = strings.TrimSpace(parts[1])
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		return PublicKey{}, fmt.Errorf("the public key is not valid base64: %w", err)
	}
	if len(raw) != pubKeyLen {
		return PublicKey{}, fmt.Errorf("the public key is %d bytes long; a minisign public key is %d bytes", len(raw), pubKeyLen)
	}
	if string(raw[:2]) != algLegacy {
		return PublicKey{}, errors.New("the public key is not an Ed25519 minisign key")
	}
	var pk PublicKey
	copy(pk.ID[:], raw[2:10])
	pk.Key = ed25519.PublicKey(bytes.Clone(raw[10:]))
	return pk, nil
}

// String returns the one-line base64 form.
func (p PublicKey) String() string {
	raw := make([]byte, 0, pubKeyLen)
	raw = append(raw, algLegacy...)
	raw = append(raw, p.ID[:]...)
	raw = append(raw, p.Key...)
	return base64.StdEncoding.EncodeToString(raw)
}

// File returns the two-line .pub file content.
func (p PublicKey) File() string {
	return untrustPfx + "minisign public key " + p.ID.String() + "\n" + p.String() + "\n"
}

// GenerateKey creates a new key pair from r (crypto/rand when nil).
func GenerateKey(r io.Reader) (PublicKey, PrivateKey, error) {
	if r == nil {
		r = rand.Reader
	}
	pub, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		return PublicKey{}, PrivateKey{}, err
	}
	var id KeyID
	if _, err := io.ReadFull(r, id[:]); err != nil {
		return PublicKey{}, PrivateKey{}, err
	}
	return PublicKey{ID: id, Key: pub}, PrivateKey{ID: id, Key: priv}, nil
}

// Public returns the matching public key.
func (k PrivateKey) Public() PublicKey {
	return PublicKey{ID: k.ID, Key: k.Key.Public().(ed25519.PublicKey)}
}

// File returns the unencrypted secret key file content.
func (k PrivateKey) File() string {
	raw := make([]byte, 0, secKeyLen)
	raw = append(raw, algLegacy...)
	raw = append(raw, 0, 0) // no KDF: the key is stored unencrypted
	raw = append(raw, "B2"...)
	raw = append(raw, make([]byte, 32+8+8)...) // salt, opslimit, memlimit
	raw = append(raw, k.ID[:]...)
	raw = append(raw, k.Key...)
	sum := k.checksum()
	raw = append(raw, sum[:]...)
	return untrustPfx + "minisign secret key " + k.ID.String() + " (unencrypted)\n" +
		base64.StdEncoding.EncodeToString(raw) + "\n"
}

func (k PrivateKey) checksum() [32]byte {
	h, _ := blake2b.New256(nil)
	h.Write([]byte(algLegacy))
	h.Write(k.ID[:])
	h.Write(k.Key)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ParsePrivateKey reads an unencrypted secret key file (or its base64 line).
func ParsePrivateKey(s string) (PrivateKey, error) {
	line := ""
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, untrustPfx) {
			line = l
			break
		}
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("the secret key is not valid base64: %w", err)
	}
	if len(raw) != secKeyLen {
		return PrivateKey{}, fmt.Errorf("the secret key is %d bytes long; a minisign secret key is %d bytes", len(raw), secKeyLen)
	}
	if string(raw[:2]) != algLegacy {
		return PrivateKey{}, errors.New("the secret key is not an Ed25519 minisign key")
	}
	if raw[2] != 0 || raw[3] != 0 {
		return PrivateKey{}, errors.New("the secret key is encrypted with a password; this tool needs an unencrypted key (minisign -G -W)")
	}
	if string(raw[4:6]) != "B2" {
		return PrivateKey{}, errors.New("the secret key uses an unknown checksum algorithm")
	}
	off := 6 + 32 + 8 + 8
	var k PrivateKey
	copy(k.ID[:], raw[off:off+8])
	k.Key = ed25519.PrivateKey(bytes.Clone(raw[off+8 : off+8+ed25519.PrivateKeySize]))
	// minisign -W leaves the checksum zeroed; anything else must match.
	got := raw[off+8+ed25519.PrivateKeySize:]
	want := k.checksum()
	if !bytes.Equal(got, make([]byte, 32)) && !bytes.Equal(want[:], got) {
		return PrivateKey{}, errors.New("the secret key checksum does not match, so the file is damaged")
	}
	return k, nil
}

// Sign signs message and returns the .minisig file content. The trusted
// comment is covered by the signature; it must be a single line.
func Sign(k PrivateKey, message []byte, trustedComment string) (string, error) {
	if strings.ContainsAny(trustedComment, "\r\n") {
		return "", errors.New("the trusted comment must be a single line")
	}
	sum := blake2b.Sum512(message)
	sig := ed25519.Sign(k.Key, sum[:])
	sigLine := make([]byte, 0, sigLen)
	sigLine = append(sigLine, algHashed...)
	sigLine = append(sigLine, k.ID[:]...)
	sigLine = append(sigLine, sig...)
	global := ed25519.Sign(k.Key, append(bytes.Clone(sig), trustedComment...))
	return untrustPfx + "signature from minisign secret key\n" +
		base64.StdEncoding.EncodeToString(sigLine) + "\n" +
		trustedPfx + trustedComment + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n", nil
}

// ParseSignature reads a .minisig file.
func ParseSignature(b []byte) (Signature, error) {
	if len(b) > maxSigBytes {
		return Signature{}, errors.New("the signature file is too large to be a minisign signature")
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) < 4 {
		return Signature{}, errors.New("the signature file has fewer than the four lines a minisign signature has")
	}
	var s Signature
	if !strings.HasPrefix(lines[0], untrustPfx) {
		return Signature{}, errors.New("the signature file does not start with an untrusted comment line")
	}
	s.UntrustedComment = strings.TrimPrefix(lines[0], untrustPfx)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != sigLen {
		return Signature{}, errors.New("the signature line is not a valid minisign signature")
	}
	s.Algorithm = string(raw[:2])
	if s.Algorithm != algLegacy && s.Algorithm != algHashed {
		return Signature{}, fmt.Errorf("the signature uses the unknown algorithm %q", s.Algorithm)
	}
	copy(s.KeyID[:], raw[2:10])
	s.Signature = raw[10:]
	if !strings.HasPrefix(lines[2], trustedPfx) {
		return Signature{}, errors.New("the signature file has no trusted comment line")
	}
	s.TrustedComment = strings.TrimPrefix(lines[2], trustedPfx)
	s.GlobalSignature, err = base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(s.GlobalSignature) != ed25519.SignatureSize {
		return Signature{}, errors.New("the global signature line is not valid")
	}
	return s, nil
}

// ErrUnknownKey means the signature was made by a key that is not trusted.
var ErrUnknownKey = errors.New("the signature was made with a key this build does not trust")

// Verify checks sig over message against any of keys and returns the key
// that matched. Both the file signature and the trusted comment must verify.
func Verify(keys []PublicKey, message, sigFile []byte) (PublicKey, Signature, error) {
	s, err := ParseSignature(sigFile)
	if err != nil {
		return PublicKey{}, Signature{}, err
	}
	var key *PublicKey
	for i := range keys {
		if keys[i].ID == s.KeyID {
			key = &keys[i]
			break
		}
	}
	if key == nil {
		return PublicKey{}, s, fmt.Errorf("%w (key id %s)", ErrUnknownKey, s.KeyID)
	}
	signed := message
	if s.Algorithm == algHashed {
		sum := blake2b.Sum512(message)
		signed = sum[:]
	}
	if !ed25519.Verify(key.Key, signed, s.Signature) {
		return PublicKey{}, s, errors.New("the signature does not match the file, so the file was changed after it was signed")
	}
	if !ed25519.Verify(key.Key, append(bytes.Clone(s.Signature), s.TrustedComment...), s.GlobalSignature) {
		return PublicKey{}, s, errors.New("the trusted comment of the signature was changed after signing")
	}
	return *key, s, nil
}

// Fingerprint returns a short hex digest of a public key, for log lines.
func (p PublicKey) Fingerprint() string {
	sum := blake2b.Sum256(p.Key)
	return hex.EncodeToString(sum[:4])
}
