// Package auth implements the OAuth 2.0 device-code flow against the
// Microsoft identity platform, with a refresh-token cache on disk.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Scopes requested for Teams functionality. Some (ChannelMessage.Read.All,
// ChannelMessage.ReadWrite) may require tenant admin consent.
var Scopes = []string{
	"offline_access",
	"User.Read",
	"User.ReadBasic.All",
	"People.Read",
	"Presence.Read.All",
	"Presence.ReadWrite",
	"Chat.ReadWrite",
	"ChatMember.ReadWrite",
	"ChatMessage.Send",
	"Team.ReadBasic.All",
	"Channel.ReadBasic.All",
	"ChannelMember.Read.All",
	"ChannelMessage.Read.All",
	"ChannelMessage.Send",
	"ChannelMessage.ReadWrite",
	"Calendars.ReadWrite",
	"Files.ReadWrite.All",
	"Sites.Read.All",
	"OnlineMeetings.Read",
	"OnlineMeetingTranscript.Read.All",
	"OnlineMeetingRecording.Read.All",
}

// ErrNotLoggedIn is returned when no cached token exists.
var ErrNotLoggedIn = errors.New("not logged in")

// Token is the cached token set.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Scope        string    `json:"scope"`
}

// Authenticator obtains and refreshes access tokens.
type Authenticator struct {
	ClientID string
	Tenant   string
	path     string
	http     *http.Client

	mu  sync.Mutex
	tok *Token
}

// New creates an authenticator caching tokens in dir/token.json.
func New(clientID, tenant, dir string) *Authenticator {
	a := &Authenticator{
		ClientID: clientID,
		Tenant:   tenant,
		path:     filepath.Join(dir, "token.json"),
		http:     &http.Client{Timeout: 30 * time.Second},
	}
	if b, err := os.ReadFile(a.path); err == nil {
		var t Token
		if json.Unmarshal(b, &t) == nil && t.RefreshToken != "" {
			a.tok = &t
		}
	}
	return a
}

func (a *Authenticator) endpoint(p string) string {
	return "https://login.microsoftonline.com/" + url.PathEscape(a.Tenant) + "/oauth2/v2.0/" + p
}

// LoggedIn reports whether a refresh token is cached.
func (a *Authenticator) LoggedIn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tok != nil
}

// Logout deletes the cached token.
func (a *Authenticator) Logout() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tok = nil
	err := os.Remove(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DeviceCode is the response from the devicecode endpoint.
type DeviceCode struct {
	UserCode        string `json:"user_code"`
	DeviceCode      string `json:"device_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

type tokenResp struct {
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (a *Authenticator) post(ctx context.Context, endpoint string, form url.Values) (*tokenResp, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	var tr tokenResp
	_ = json.Unmarshal(b, &tr)
	return &tr, b, nil
}

// Login runs the device-code flow. prompt is called once with the code the
// user must enter at the verification URL.
func (a *Authenticator) Login(ctx context.Context, prompt func(DeviceCode)) error {
	form := url.Values{"client_id": {a.ClientID}, "scope": {strings.Join(Scopes, " ")}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint("devicecode"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device code request failed: %s", strings.TrimSpace(string(b)))
	}
	var dc DeviceCode
	if err := json.Unmarshal(b, &dc); err != nil {
		return err
	}
	prompt(dc)

	interval := time.Duration(dc.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		tr, raw, err := a.post(ctx, a.endpoint("token"), url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {a.ClientID},
			"device_code": {dc.DeviceCode},
		})
		if err != nil {
			return err
		}
		switch tr.Error {
		case "":
			if tr.AccessToken == "" {
				return fmt.Errorf("unexpected token response: %s", raw)
			}
			return a.store(tr)
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		default:
			return fmt.Errorf("%s: %s", tr.Error, tr.ErrorDesc)
		}
	}
	return errors.New("device code expired; please try again")
}

func (a *Authenticator) store(tr *tokenResp) error {
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Scope:        tr.Scope,
	}
	if t.RefreshToken == "" && a.tok != nil {
		t.RefreshToken = a.tok.RefreshToken
	}
	if t.Scope == "" && a.tok != nil {
		t.Scope = a.tok.Scope
	}
	a.tok = t
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, b, 0o600)
}

// AccessToken returns a valid access token, refreshing when needed.
func (a *Authenticator) AccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok == nil {
		return "", ErrNotLoggedIn
	}
	if time.Until(a.tok.Expiry) > 2*time.Minute && a.tok.AccessToken != "" {
		return a.tok.AccessToken, nil
	}
	return a.refreshLocked(ctx)
}

// ForceRefresh discards the current access token and refreshes.
func (a *Authenticator) ForceRefresh(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok == nil {
		return "", ErrNotLoggedIn
	}
	return a.refreshLocked(ctx)
}

func (a *Authenticator) refreshLocked(ctx context.Context) (string, error) {
	// Refresh with the scopes already granted, so adding new scopes in a
	// later version doesn't break existing sessions (run `login` to add them).
	scope := strings.Join(Scopes, " ")
	if a.tok.Scope != "" {
		scope = a.tok.Scope
		if !strings.Contains(scope, "offline_access") {
			scope += " offline_access"
		}
	}
	tr, raw, err := a.post(ctx, a.endpoint("token"), url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {a.ClientID},
		"refresh_token": {a.tok.RefreshToken},
		"scope":         {scope},
	})
	if err != nil {
		return "", err
	}
	if tr.Error != "" || tr.AccessToken == "" {
		if tr.Error == "invalid_grant" {
			a.tok = nil
			_ = os.Remove(a.path)
			return "", fmt.Errorf("session expired, run `teams-tui login`: %s", tr.ErrorDesc)
		}
		return "", fmt.Errorf("token refresh failed: %s", raw)
	}
	if err := a.store(tr); err != nil {
		return "", err
	}
	return a.tok.AccessToken, nil
}

// Claims decodes (without verifying) the access token's JWT payload.
func (a *Authenticator) Claims() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok == nil {
		return nil
	}
	parts := strings.Split(a.tok.AccessToken, ".")
	if len(parts) < 2 {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// TenantID returns the tenant id ("tid" claim) of the signed-in user.
func (a *Authenticator) TenantID() string {
	if c := a.Claims(); c != nil {
		if s, ok := c["tid"].(string); ok {
			return s
		}
	}
	return ""
}

// HasScope reports whether the cached token was granted scope (case-insensitive).
func (a *Authenticator) HasScope(scope string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok == nil {
		return false
	}
	for _, s := range strings.Fields(a.tok.Scope) {
		s = s[strings.LastIndex(s, "/")+1:]
		if strings.EqualFold(s, scope) {
			return true
		}
	}
	return false
}
