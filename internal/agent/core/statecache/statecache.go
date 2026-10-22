// SPDX-License-Identifier: AGPL-3.0-only

// Package statecache keeps the agent's last applied desired state on disk,
// encrypted with the host key (docs/SPEC.md 6.4, 7.2). The file holds
// resolved environment variables and the registry pull credential, so it is
// sealed with AES-256-GCM and written 0600 through a temporary file and a
// rename, so a crash never leaves half a file.
//
// The key is derived from the content of /etc/5050/host.key with SHA-256,
// so the file may hold raw bytes, hex or base64: whatever 5050d init or the
// agent wrote, the same file always gives the same key.
package statecache

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// FileName is the cache file under <DataDir>/agent.
const FileName = "desired.json.enc"

var magic = []byte("5050DS1\n")

// ErrNoCache is returned by Load when there is no cache file yet.
var ErrNoCache = errors.New("no desired-state cache yet")

// Entry is what the cache stores.
type Entry struct {
	State proto.DesiredState `json:"state"`
	// AppliedAt is when the state was applied (Unix ms).
	AppliedAt int64 `json:"appliedAt"`
}

// Cache reads and writes one cache file.
type Cache struct {
	path string
	aead cipher.AEAD
}

// Path returns the cache path for a data directory.
func Path(dataDir string) string { return filepath.Join(dataDir, "agent", FileName) }

// New returns a cache at path sealed with key material (the content of
// host.key). The material must hold at least 16 bytes.
func New(path string, keyMaterial []byte) (*Cache, error) {
	km := bytes.TrimSpace(keyMaterial)
	if len(km) < 16 {
		return nil, errors.New("the host key is too short: it needs at least 16 bytes")
	}
	h := sha256.New()
	h.Write([]byte("5050cloud desired-state cache v1\x00"))
	h.Write(km)
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cache{path: path, aead: aead}, nil
}

// Save seals and writes the entry.
func (c *Cache) Save(e Entry) error {
	plain, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("could not encode the desired-state cache: %w", err)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	out := make([]byte, 0, len(magic)+len(nonce)+len(plain)+c.aead.Overhead())
	out = append(out, magic...)
	out = append(out, nonce...)
	out = c.aead.Seal(out, nonce, plain, magic)
	return WriteFileAtomic(c.path, out, 0o600)
}

// Load reads and opens the cache. It returns ErrNoCache when the file does
// not exist, and a full-sentence error when it cannot be decrypted (a
// different host key) or is damaged.
func (c *Cache) Load() (Entry, error) {
	data, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, ErrNoCache
	}
	if err != nil {
		return Entry{}, fmt.Errorf("could not read the desired-state cache %s: %w", c.path, err)
	}
	if !bytes.HasPrefix(data, magic) || len(data) < len(magic)+c.aead.NonceSize()+c.aead.Overhead() {
		return Entry{}, fmt.Errorf("the desired-state cache %s is damaged; the agent waits for the control plane instead", c.path)
	}
	rest := data[len(magic):]
	nonce, sealed := rest[:c.aead.NonceSize()], rest[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, sealed, magic)
	if err != nil {
		return Entry{}, fmt.Errorf("the desired-state cache %s cannot be decrypted with this host key; the agent waits for the control plane instead", c.path)
	}
	var e Entry
	if err := json.Unmarshal(plain, &e); err != nil {
		return Entry{}, fmt.Errorf("the desired-state cache %s is damaged: %w", c.path, err)
	}
	return e, nil
}

// Remove deletes the cache (rejoin).
func (c *Cache) Remove() error {
	err := os.Remove(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// LoadOrCreateKey reads the host key file, creating it (32 random bytes,
// hex, 0600) when it does not exist.
func LoadOrCreateKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if len(bytes.TrimSpace(b)) < 16 {
			return nil, fmt.Errorf("the host key %s is too short; delete it to let the agent create a new one", path)
		}
		return b, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("could not read the host key %s: %w", path, err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	key := []byte(hex.EncodeToString(raw) + "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("could not create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return LoadOrCreateKey(path) // created by someone else in between
	}
	if err != nil {
		return nil, fmt.Errorf("could not create the host key %s: %w", path, err)
	}
	_, werr := f.Write(key)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, fmt.Errorf("could not write the host key %s: %w", path, werr)
	}
	return key, nil
}

// WriteFileAtomic writes data to path through a temporary file in the same
// directory, fsyncs it and renames it into place.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
