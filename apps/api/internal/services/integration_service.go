package services

// integration_service.go implements the OAuth2 "Connect Gmail" flow (option 2:
// send mail AS the user's own Google account). The shape mirrors how n8n/Zapier
// connect Gmail:
//
//	1. GET /api/integrations/google/connect  → 302 to Google's consent screen.
//	2. user approves; Google redirects back to .../callback?code=...&state=...
//	3. callback exchanges the code for a REFRESH TOKEN (+ the account email),
//	   stores it in the integrations table, and bounces the browser back to the app.
//
// The worker later reads that refresh token (per workspace) and sends mail via the
// Gmail API — no password ever touches Agently. See worker internal/notifier
// (gmail-oauth channel) and queue.LoadGoogleIntegration.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/agently/api/internal/domain"
)

// Gmail send + identity. gmail.send is the least-privilege scope that lets us send
// as the user; openid/email let us read back which account was connected.
const googleScopes = "https://www.googleapis.com/auth/gmail.send openid email"

// IntegrationService runs the OAuth dance and persists connections.
type IntegrationService struct {
	deps         Deps
	clientID     string
	clientSecret string
	redirectURL  string
	appBaseURL   string
	http         *http.Client

	mu     sync.Mutex
	states map[string]bool // CSRF states issued by /connect, consumed by /callback
}

func NewIntegrationService(deps Deps, http *http.Client) *IntegrationService {
	return &IntegrationService{
		deps:         deps,
		clientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		clientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		redirectURL:  envOrDefault("GOOGLE_OAUTH_REDIRECT", "http://localhost:8080/api/integrations/google/callback"),
		appBaseURL:   envOrDefault("APP_BASE_URL", "http://localhost:3000"),
		http:         http,
		states:       map[string]bool{},
	}
}

// Configured reports whether the OAuth app credentials are present. When false,
// /connect returns a clear error instead of a broken redirect.
func (s *IntegrationService) Configured() bool {
	return s.clientID != "" && s.clientSecret != ""
}

// AuthURL builds the Google consent URL and remembers the CSRF state. access_type
// =offline + prompt=consent guarantees Google returns a refresh token (not just an
// access token) every time.
func (s *IntegrationService) AuthURL() (string, error) {
	if !s.Configured() {
		return "", domain.Errorf(domain.CodeValidationFailed, "Google OAuth is not configured (set GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET)")
	}
	state := randToken()
	s.mu.Lock()
	s.states[state] = true
	s.mu.Unlock()

	q := url.Values{
		"client_id":     {s.clientID},
		"redirect_uri":  {s.redirectURL},
		"response_type": {"code"},
		"scope":         {googleScopes},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(), nil
}

// AppRedirect is where the browser is sent after the callback finishes. status is
// "connected" or "error".
func (s *IntegrationService) AppRedirect(status string) string {
	return s.appBaseURL + "/settings?google=" + url.QueryEscape(status)
}

// HandleCallback exchanges the auth code for tokens, reads the connected account's
// email, and stores the integration for the current workspace.
func (s *IntegrationService) HandleCallback(ctx context.Context, code, state string) error {
	s.mu.Lock()
	ok := s.states[state]
	delete(s.states, state)
	s.mu.Unlock()
	if !ok {
		return domain.Errorf(domain.CodeValidationFailed, "invalid or expired OAuth state")
	}
	if code == "" {
		return domain.Errorf(domain.CodeValidationFailed, "missing authorization code")
	}

	tok, err := s.exchangeCode(ctx, code)
	if err != nil {
		return err
	}
	if tok.RefreshToken == "" {
		// Google omits the refresh token if the user previously consented without
		// revoking. prompt=consent above prevents this; surfaced clearly just in case.
		return domain.Errorf(domain.CodeValidationFailed, "Google did not return a refresh token; revoke Agently at myaccount.google.com/permissions and retry")
	}
	email := emailFromIDToken(tok.IDToken)

	ws := s.deps.Repos.Workspace.Get()
	s.deps.Repos.Integrations.Upsert(domain.Integration{
		ID:           domain.NewIntegrationId(),
		WorkspaceID:  ws.ID,
		Provider:     "google",
		AccountEmail: email,
		RefreshToken: tok.RefreshToken,
		Scopes:       googleScopes,
		CreatedAt:    domain.Timestamp(s.deps.Clock.ISO()),
		UpdatedAt:    domain.Timestamp(s.deps.Clock.ISO()),
	})
	s.deps.Logger.Info("google account connected", "workspace", string(ws.ID), "account", email)
	return nil
}

// Status is the UI read model: is Google connected, and as which account.
type IntegrationStatus struct {
	Provider     string `json:"provider"`
	Connected    bool   `json:"connected"`
	AccountEmail string `json:"accountEmail,omitempty"`
	Configured   bool   `json:"configured"` // whether the OAuth app creds exist at all
}

func (s *IntegrationService) Status(provider string) IntegrationStatus {
	ws := s.deps.Repos.Workspace.Get()
	st := IntegrationStatus{Provider: provider, Configured: s.Configured()}
	if it, ok := s.deps.Repos.Integrations.GetByWorkspaceProvider(ws.ID, provider); ok {
		st.Connected = it.RefreshToken != ""
		st.AccountEmail = it.AccountEmail
	}
	return st
}

// Disconnect removes a stored connection.
func (s *IntegrationService) Disconnect(provider string) bool {
	ws := s.deps.Repos.Workspace.Get()
	return s.deps.Repos.Integrations.DeleteByWorkspaceProvider(ws.ID, provider)
}

/* ----------------------------- internals ----------------------------- */

type googleToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *IntegrationService) exchangeCode(ctx context.Context, code string) (googleToken, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
		"redirect_uri":  {s.redirectURL},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return googleToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		return googleToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return googleToken{}, domain.Errorf(domain.CodeInternal, "Google token exchange failed (status %d)", resp.StatusCode)
	}
	var tok googleToken
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return googleToken{}, err
	}
	return tok, nil
}

// emailFromIDToken extracts the "email" claim from a Google id_token (a JWT). Best
// effort — returns "" if the token is absent or unparseable (the connection still
// works for sending; we just don't know the address to display).
func emailFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Email
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
