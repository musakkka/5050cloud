// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/pkg/api"
)

// s3Client is a small S3 client (Signature Version 4) for the bucket test.
// Backups themselves are written by WAL-G and restic inside jobs.
type s3Client struct {
	endpoint  *url.URL
	bucket    string
	region    string
	pathStyle bool
	keyID     string
	secret    string
	http      *http.Client
	now       func() time.Time
}

func newS3Client(b api.BackupSettings, keyID, secret string, hc *http.Client) (*s3Client, error) {
	u, err := url.Parse(strings.TrimRight(b.Endpoint, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("The bucket endpoint %q must be a URL such as https://s3.us-east-1.amazonaws.com.", b.Endpoint)
	}
	region := b.Region
	if region == "" {
		region = "us-east-1"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &s3Client{endpoint: u, bucket: b.Bucket, region: region, pathStyle: b.PathStyle,
		keyID: keyID, secret: secret, http: hc, now: time.Now}, nil
}

func (c *s3Client) objectURL(key string) *url.URL {
	u := *c.endpoint
	u.RawPath = ""
	if c.pathStyle {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + c.bucket + "/" + key
	} else {
		u.Host = c.bucket + "." + u.Host
		u.Path = strings.TrimRight(u.Path, "/") + "/" + key
	}
	return &u
}

// do signs and sends one request and returns the body of a 2xx response.
func (c *s3Client) do(ctx context.Context, method, key string, body []byte) ([]byte, error) {
	u := c.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	now := c.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if body != nil {
		req.ContentLength = int64(len(body))
	}

	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(signed)
	var canonHeaders strings.Builder
	for _, h := range signed {
		v := req.Header.Get(h)
		if h == "host" {
			v = u.Host
		}
		canonHeaders.WriteString(h + ":" + strings.TrimSpace(v) + "\n")
	}
	canonical := strings.Join([]string{
		method,
		u.EscapedPath(),
		"",
		canonHeaders.String(),
		strings.Join(signed, ";"),
		payloadHash,
	}, "\n")
	scope := day + "/" + c.region + "/s3/aws4_request"
	creq := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(creq[:])
	k := hmacSHA256([]byte("AWS4"+c.secret), day)
	k = hmacSHA256(k, c.region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.keyID+"/"+scope+
		", SignedHeaders="+strings.Join(signed, ";")+", Signature="+sig)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("The bucket endpoint %s cannot be reached: %w.", c.endpoint.Host, unwrapURLError(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, &s3Error{Status: resp.StatusCode, Code: s3ErrorCode(data), Message: s3ErrorMessage(data)}
	}
	return data, nil
}

func hmacSHA256(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

type s3Error struct {
	Status  int
	Code    string
	Message string
}

func (e *s3Error) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("%s (%s, HTTP %d)", strings.TrimSuffix(e.Message, "."), e.Code, e.Status)
	case e.Code != "":
		return fmt.Sprintf("%s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func s3ErrorCode(body []byte) string {
	var x struct {
		Code string `xml:"Code"`
	}
	_ = xml.Unmarshal(body, &x)
	return x.Code
}

func s3ErrorMessage(body []byte) string {
	var x struct {
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(body, &x)
	return x.Message
}

// testBucket puts and gets a small object with the write key and deletes it
// with the retention key (docs/SPEC.md 9.13, Test connection). Without a
// retention key it tries the delete with the write key and accepts a refusal,
// since a write key without delete rights is the recommended setup.
func testBucket(ctx context.Context, b api.BackupSettings, hc *http.Client) api.TestResult {
	if err := validateBucket(b); err != nil {
		return api.TestResult{Error: err.Error()}
	}
	w, err := newS3Client(b, b.AccessKeyID, b.SecretAccessKey, hc)
	if err != nil {
		return api.TestResult{Error: err.Error()}
	}
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	key := strings.TrimLeft(strings.Trim(b.Prefix, "/")+"/_5050/test-"+hex.EncodeToString(nonce[:]), "/")
	body := []byte("5050cloud bucket test " + hex.EncodeToString(nonce[:]) + "\n")

	if _, err := w.do(ctx, http.MethodPut, key, body); err != nil {
		return api.TestResult{Error: fmt.Sprintf("The write key could not put a test object into %s: %v.", b.Bucket, strings.TrimSuffix(err.Error(), "."))}
	}
	got, err := w.do(ctx, http.MethodGet, key, nil)
	if err != nil {
		return api.TestResult{Error: fmt.Sprintf("The write key could not read the test object back from %s: %v.", b.Bucket, strings.TrimSuffix(err.Error(), "."))}
	}
	if !bytes.Equal(got, body) {
		return api.TestResult{Error: "The bucket returned different bytes than were written; check the endpoint and path style."}
	}
	if retentionReady(b) {
		r, err := newS3Client(b, b.RetentionAccessKeyID, b.RetentionSecretAccessKey, hc)
		if err != nil {
			return api.TestResult{Error: err.Error()}
		}
		if _, err := r.do(ctx, http.MethodDelete, key, nil); err != nil {
			return api.TestResult{Error: fmt.Sprintf("The retention key could not delete the test object: %v.", strings.TrimSuffix(err.Error(), "."))}
		}
		return api.TestResult{OK: true}
	}
	// No retention key: a refusal here is the safe setup, not a failure.
	_, _ = w.do(ctx, http.MethodDelete, key, nil)
	return api.TestResult{OK: true}
}
