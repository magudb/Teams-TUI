package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheIdentity(t *testing.T) {
	dir := t.TempDir()
	a := New("client-a", "first", dir)
	if err := a.store(&tokenResp{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600}, "second", false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		client, tenant string
		want           bool
	}{{"CLIENT-A", "SECOND", true}, {"client-b", "second", false}, {"client-a", "first", false}} {
		if got := New(tc.client, tc.tenant, dir).LoggedIn(); got != tc.want {
			t.Errorf("login for %s/%s = %v", tc.client, tc.tenant, got)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"refresh_token":"legacy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if New("client-a", "second", dir).LoggedIn() {
		t.Fatal("cache without identity accepted")
	}
}

type deviceTransport func(*http.Request) (*http.Response, error)

func (f deviceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeviceSignInCodePair(t *testing.T) {
	a := New("client", "organizations", t.TempDir())
	polls := 0
	a.http.Transport = deviceTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.FormValue("client_id") != "client" {
			return nil, fmt.Errorf("unexpected authorization request")
		}
		status := http.StatusOK
		var body string
		switch r.URL.Path {
		case "/organizations/oauth2/v2.0/devicecode":
			body = `{"user_code":"TESTCODE","device_code":"opaque-test-value","verification_uri":"https://example.test/verify","expires_in":60,"interval":1}`
		case "/organizations/oauth2/v2.0/token":
			if r.FormValue("device_code") != "opaque-test-value" {
				return nil, fmt.Errorf("polling used the wrong code")
			}
			polls++
			if polls == 1 {
				status = http.StatusBadRequest
				body = `{"error":"authorization_pending"}`
			} else {
				body = `{"access_token":"test","refresh_token":"test","expires_in":3600}`
			}
		default:
			return nil, fmt.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	prompts := 0
	err := a.Login(context.Background(), func(dc DeviceCode) {
		prompts++
		if dc.UserCode != "TESTCODE" || dc.VerificationURI != "https://example.test/verify" {
			t.Fatal("incorrect browser instructions")
		}
	})
	if err != nil || prompts != 1 || polls != 2 || !a.LoggedIn() {
		t.Fatalf("prompts=%d polls=%d login=%v err=%v", prompts, polls, a.LoggedIn(), err)
	}
}
