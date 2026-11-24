// Package graph implements model.Service on top of Microsoft Graph.
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/magudb/Teams-TUI/internal/auth"
)

var base = "https://graph.microsoft.com/v1.0"

// Client talks to Microsoft Graph.
type Client struct {
	auth *auth.Authenticator
	http *http.Client
	me   string
}

// New returns a Graph client.
func New(a *auth.Authenticator) *Client {
	return &Client{auth: a, http: &http.Client{Timeout: 60 * time.Second}}
}

// APIError is a Graph error response.
type APIError struct {
	Status  int
	Code    string
	Message string
	Path    string
}

func (e *APIError) Error() string {
	msg := e.Message
	if e.Status == http.StatusForbidden {
		msg += " (missing permission / admin consent?)"
	}
	return fmt.Sprintf("graph %d %s: %s", e.Status, e.Code, msg)
}

// IsStatus reports whether err is an APIError with the given status.
func IsStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == status
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

// do performs a request. path may be relative to base or an absolute URL.
// body may be nil, an io.Reader (sent as-is) or a value marshalled to JSON.
func (c *Client) do(ctx context.Context, method, path string, body any, out any, opts ...reqOpt) error {
	resp, err := c.raw(ctx, method, path, body, opts...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

func (c *Client) raw(ctx context.Context, method, path string, body any, opts ...reqOpt) (*http.Response, error) {
	u := path
	if !strings.HasPrefix(u, "http") {
		u = base + path
	}
	var payload []byte
	var rdr io.Reader
	isJSON := false
	switch b := body.(type) {
	case nil:
	case io.Reader:
		rdr = b
	case []byte:
		payload = b
	default:
		var err error
		payload, err = json.Marshal(b)
		if err != nil {
			return nil, err
		}
		isJSON = true
	}

	refreshed := false
	for attempt := 0; attempt < 4; attempt++ {
		tok, err := c.auth.AccessToken(ctx)
		if err != nil {
			return nil, err
		}
		var r io.Reader = rdr
		if payload != nil {
			r = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, r)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(u, base) || strings.HasPrefix(u, "https://graph.microsoft.com") {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if isJSON {
			req.Header.Set("Content-Type", "application/json")
		}
		for _, o := range opts {
			o(req)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized && !refreshed:
			resp.Body.Close()
			refreshed = true
			if _, err := c.auth.ForceRefresh(ctx); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
			resp.Body.Close()
			wait := 2 * time.Second
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = time.Duration(n) * time.Second
				}
			}
			if rdr != nil { // streaming body cannot be retried
				return nil, &APIError{Status: resp.StatusCode, Code: "throttled", Message: "try again later", Path: path}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		case resp.StatusCode >= 400:
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			var e struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(b, &e)
			if e.Error.Message == "" {
				e.Error.Message = strings.TrimSpace(string(b))
			}
			return nil, &APIError{Status: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message, Path: path}
		}
		return resp, nil
	}
	return nil, &APIError{Status: 429, Code: "throttled", Message: "too many retries", Path: path}
}

// list follows @odata.nextLink up to maxPages pages.
func list[T any](ctx context.Context, c *Client, path string, maxPages int, opts ...reqOpt) ([]T, error) {
	var all []T
	next := path
	for i := 0; next != "" && i < maxPages; i++ {
		var page struct {
			Value    []T    `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := c.do(ctx, http.MethodGet, next, nil, &page, opts...); err != nil {
			return all, err
		}
		all = append(all, page.Value...)
		next = page.NextLink
	}
	return all, nil
}

// FetchURL downloads an authenticated Graph URL (e.g. hosted content) to dest.
func (c *Client) FetchURL(ctx context.Context, url, dest string) error {
	resp, err := c.raw(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return saveTo(resp.Body, dest)
}

func saveTo(r io.Reader, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// TenantID returns the signed-in tenant id.
func (c *Client) TenantID() string { return c.auth.TenantID() }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
