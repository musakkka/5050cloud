// SPDX-License-Identifier: MIT

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

// Client talks to one 5050d instance over the REST API (SPEC section 11).
type Client struct {
	Base  string // instance URL, no trailing slash
	Token string
	HTTP  *http.Client
}

// APIError is a non-2xx answer.
type APIError struct {
	Status int
	Body   api.Error
}

func (e *APIError) Error() string {
	if e.Body.Error != "" {
		return e.Body.Error
	}
	return fmt.Sprintf("The server answered %d %s.", e.Status, http.StatusText(e.Status))
}

// IsCode reports whether err is an APIError with the given code.
func IsCode(err error, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Body.Code == code
}

func newClient(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{}}
}

func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "/healthz") || strings.HasPrefix(path, "/readyz") {
		return c.Base + path
	}
	return c.Base + "/api/v1" + path
}

func (c *Client) newReq(ctx context.Context, method, path string, body io.Reader, ctype string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if method != http.MethodGet {
		req.Header.Set("X-5050-CSRF", "1")
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("User-Agent", "5050-cli/"+version.Version)
	return req, nil
}

func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", c.Base, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		ae := &APIError{Status: resp.StatusCode}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if json.Unmarshal(b, &ae.Body) != nil || ae.Body.Error == "" {
			ae.Body.Error = strings.TrimSpace(string(b))
		}
		if resp.StatusCode == http.StatusUnauthorized && ae.Body.Code == "" {
			ae.Body.Code = api.CodeUnauthorized
		}
		return nil, ae
	}
	return resp, nil
}

// Do sends in as JSON (nil for no body) and decodes the answer into out (nil to discard).
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	ctype := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		ctype = "application/json"
	}
	return c.DoRaw(ctx, method, path, body, ctype, out)
}

// DoRaw sends a raw body. out may be *[]byte to receive the raw answer.
func (c *Client) DoRaw(ctx context.Context, method, path string, body io.Reader, ctype string, out any) error {
	req, err := c.newReq(ctx, method, path, body, ctype)
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw, err = io.ReadAll(resp.Body)
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("could not read the answer from %s: %w", path, err)
	}
	return nil
}

// SSE streams Server-Sent Events from path and calls fn with each event's
// type and data until the stream ends, ctx is done or fn returns false.
func (c *Client) SSE(ctx context.Context, path string, fn func(event string, data []byte) bool) error {
	req, err := c.newReq(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return readSSE(resp.Body, fn)
}

func readSSE(r io.Reader, fn func(event string, data []byte) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	var event string
	var data bytes.Buffer
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if data.Len() > 0 {
				if !fn(event, bytes.TrimSuffix(data.Bytes(), []byte("\n"))) {
					return nil
				}
			}
			event = ""
			data.Reset()
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			data.WriteByte('\n')
		}
	}
	if data.Len() > 0 {
		fn(event, bytes.TrimSuffix(data.Bytes(), []byte("\n")))
	}
	if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// Dial opens a WebSocket to path (shell and tunnel routes).
func (c *Client) Dial(ctx context.Context, path string) (*websocket.Conn, error) {
	u := c.url(path)
	h := http.Header{}
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
	h.Set("User-Agent", "5050-cli/"+version.Version)
	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: h})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode >= 300 {
			return nil, &APIError{Status: resp.StatusCode}
		}
		return nil, fmt.Errorf("could not open %s: %w", path, err)
	}
	conn.SetReadLimit(32 << 20)
	return conn, nil
}

// q builds a query string, skipping empty values.
func q(kv ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			v.Set(kv[i], kv[i+1])
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
