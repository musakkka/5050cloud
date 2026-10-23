// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/musakkka/5050cloud/pkg/api"
)

// BackupKeySource is an optional interface of cpapi.Config. Core owns the
// master key (the recovery key) and the recovery kit, so it is the right place
// to keep the WAL-G libsodium key and the restic password stable across a
// master key rotation. When core does not implement it, the data services
// derive both keys from the master key file (see masterKeyFile).
type BackupKeySource interface {
	// BackupKeys returns the 32-byte WAL-G libsodium key as 64 hex characters
	// and the restic repository password.
	BackupKeys(ctx context.Context) (walgKeyHex, resticPassword string, err error)
}

// backupKeys are the encryption keys of every backup in the bucket.
type backupKeys struct {
	walgHex string
	restic  string
}

// defaultMasterKeyFile is where the installer writes the master key (6.4).
const defaultMasterKeyFile = "/etc/5050/master.key"

// keyring caches the backup keys for the life of the process.
type keyring struct {
	cfg  any
	file string

	mu   sync.Mutex
	keys *backupKeys
}

func (k *keyring) get(ctx context.Context) (backupKeys, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.keys != nil {
		return *k.keys, nil
	}
	if src, ok := k.cfg.(BackupKeySource); ok {
		w, r, err := src.BackupKeys(ctx)
		if err != nil {
			return backupKeys{}, fmt.Errorf("read the backup encryption keys: %w", err)
		}
		if b, err := hex.DecodeString(w); err != nil || len(b) != 32 || r == "" {
			return backupKeys{}, errors.New("The control plane returned backup keys in the wrong shape: the WAL-G key must be 64 hex characters and the restic password must not be empty.")
		}
		k.keys = &backupKeys{walgHex: w, restic: r}
		return *k.keys, nil
	}
	master, err := os.ReadFile(k.file)
	if err != nil {
		return backupKeys{}, fmt.Errorf("Backups need the master key at %s to derive their encryption keys, and it cannot be read: %w", k.file, err)
	}
	keys, err := deriveBackupKeys(master)
	if err != nil {
		return backupKeys{}, err
	}
	k.keys = &keys
	return keys, nil
}

// deriveBackupKeys derives the WAL-G key and the restic password from the
// master key with HKDF-SHA256 and fixed labels, so the same recovery key
// always opens the same backups (docs/SPEC.md 9.13).
func deriveBackupKeys(master []byte) (backupKeys, error) {
	if len(master) < 32 {
		return backupKeys{}, fmt.Errorf("The master key is %d bytes; it must be 32 bytes to derive backup keys.", len(master))
	}
	walg, err := hkdf.Key(sha256.New, master, nil, "5050cloud wal-g libsodium key v1", 32)
	if err != nil {
		return backupKeys{}, err
	}
	restic, err := hkdf.Key(sha256.New, master, nil, "5050cloud restic password v1", 32)
	if err != nil {
		return backupKeys{}, err
	}
	return backupKeys{walgHex: hex.EncodeToString(walg), restic: base64.RawURLEncoding.EncodeToString(restic)}, nil
}

// bucketReady reports whether the settings hold enough to write backups.
func bucketReady(b api.BackupSettings) bool {
	return b.Endpoint != "" && b.Bucket != "" && b.AccessKeyID != "" && b.SecretAccessKey != ""
}

// retentionReady reports whether a separate key with delete rights is set.
func retentionReady(b api.BackupSettings) bool {
	return b.RetentionAccessKeyID != "" && b.RetentionSecretAccessKey != ""
}

// servicePrefix is "<install prefix>/<service id>" inside the bucket.
func servicePrefix(b api.BackupSettings, serviceID string) string {
	p := strings.Trim(b.Prefix, "/")
	if p == "" {
		return serviceID
	}
	return p + "/" + serviceID
}

// validateBucket checks the settings before they are used in a job.
func validateBucket(b api.BackupSettings) error {
	if b.Endpoint == "" || b.Bucket == "" {
		return errors.New("Set the bucket endpoint and name in Settings > Backups.")
	}
	u, err := url.Parse(b.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("The bucket endpoint %q must be a URL such as https://s3.us-east-1.amazonaws.com.", b.Endpoint)
	}
	if strings.ContainsAny(b.Bucket, "/ ") {
		return fmt.Errorf("The bucket name %q must not contain slashes or spaces.", b.Bucket)
	}
	if b.AccessKeyID == "" || b.SecretAccessKey == "" {
		return errors.New("Set the write key (access key id and secret) in Settings > Backups.")
	}
	return nil
}

// walOnlyEnv drops the restic password from an environment that is handed to a
// Postgres container. Postgres archives and backs up with WAL-G only, so the
// restic repositories of every other service stay unreadable from a tenant's
// own database (the container env is visible to anyone who can run code in it).
func walOnlyEnv(env map[string]string) map[string]string {
	delete(env, "RESTIC_PASSWORD")
	return env
}

// backupEnv is what every backup, restore and retention command gets
// (manifest.BackupRecipe): the bucket, the prefix of one service, a key and
// the encryption settings. retention selects the retention key (delete
// rights); every other job gets the write key, which can only put and get.
func backupEnv(b api.BackupSettings, keys backupKeys, serviceID string, retention bool) map[string]string {
	prefix := servicePrefix(b, serviceID)
	region := b.Region
	if region == "" {
		region = "us-east-1"
	}
	lookup := "auto"
	if b.PathStyle {
		lookup = "path"
	}
	keyID, secret := b.AccessKeyID, b.SecretAccessKey
	if retention && retentionReady(b) {
		keyID, secret = b.RetentionAccessKeyID, b.RetentionSecretAccessKey
	}
	endpoint := strings.TrimRight(b.Endpoint, "/")
	return map[string]string{
		"BACKUP_S3_ENDPOINT":      endpoint,
		"BACKUP_S3_BUCKET":        b.Bucket,
		"BACKUP_S3_REGION":        region,
		"BACKUP_S3_PREFIX":        prefix,
		"BACKUP_S3_BUCKET_LOOKUP": lookup,
		"AWS_ACCESS_KEY_ID":       keyID,
		"AWS_SECRET_ACCESS_KEY":   secret,
		"AWS_REGION":              region,
		"AWS_DEFAULT_REGION":      region,
		// WAL-G reads its storage from these.
		"AWS_ENDPOINT":                 endpoint,
		"AWS_S3_FORCE_PATH_STYLE":      fmt.Sprint(b.PathStyle),
		"WALG_S3_PREFIX":               "s3://" + b.Bucket + "/" + prefix + "/wal-g",
		"WALG_LIBSODIUM_KEY":           keys.walgHex,
		"WALG_LIBSODIUM_KEY_TRANSFORM": "hex",
		"WALG_COMPRESSION_METHOD":      "lz4",
		"RESTIC_PASSWORD":              keys.restic,
	}
}
