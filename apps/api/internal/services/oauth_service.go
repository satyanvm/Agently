package services

// oauth_service.go runs the OAuth2 authorization-code flow for any credential
// type whose piece declares one (packages/nodes/pieces/index.json auth.oauth),
// so a node that needs a login gets a "Sign in with X" popup instead of a
// pasted access token:
//
//	1. GET /api/oauth/{type}/start  → 302 to the provider's consent screen
//	2. provider redirects to        → GET /api/oauth/callback?code&state
//	3. callback exchanges the code, stores (or updates) the credential, and
//	   bounces the popup to the web app's /oauth/done page, which tells the
//	   opener which credential was connected and closes itself.
//
// Each provider needs an OAuth app registered by the operator. Client creds come
// from OAUTH_<SLUG>_CLIENT_ID / OAUTH_<SLUG>_CLIENT_SECRET (slug = the piece
// slug upper-cased, dashes → underscores, e.g. OAUTH_GOOGLE_SHEETS_CLIENT_ID);
// every Google piece falls back to the GOOGLE_CLIENT_ID/SECRET pair the Gmail
// integration already uses, every Microsoft piece to MICROSOFT_CLIENT_ID/SECRET
// (oauth_apps.go oauthFamily). The stored credential keeps token_url + expires_at
// so the pieces worker can refresh an expired access token at run time.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agently/api/internal/domain"
)

// PieceOAuth is the index's auth.oauth block. URLs and scopes may carry
// {propKey} placeholders filled from the credential's connection props.
type PieceOAuth struct {
	AuthURL             string            `json:"authUrl"`
	TokenURL            string            `json:"tokenUrl"`
	Scope               []string          `json:"scope"`
	PKCE                bool              `json:"pkce"`
	AuthorizationMethod string            `json:"authorizationMethod"`
	Extra               map[string]string `json:"extra"`
}

// OAuthAppStatus tells the credential modal whether "Sign in" will work and,
// when it won't, exactly which env vars the operator must set.
type OAuthAppStatus struct {
	Type         string `json:"type"`
	Supported    bool   `json:"supported"`
	Configured   bool   `json:"configured"`
	RedirectURL  string `json:"redirectUrl"`
	ClientIDEnv  string `json:"clientIdEnv,omitempty"`
	ClientSecEnv string `json:"clientSecretEnv,omitempty"`
}

type oauthState struct {
	credType     string
	name         string
	credentialID string
	props        map[string]any
	verifier     string
	tokenURL     string
	issued       time.Time
}

const oauthStateTTL = 15 * time.Minute

type OAuthService struct {
	deps        Deps
	creds       *CredentialService
	http        *http.Client
	redirectURL string
	appBaseURL  string

	mu     sync.Mutex
	states map[string]oauthState

	loadOnce  sync.Once
	providers map[string]PieceOAuth // credential type id → oauth block
}

func NewOAuthService(deps Deps, creds *CredentialService, client *http.Client) *OAuthService {
	app := envOrDefault("APP_BASE_URL", "http://localhost:3000")
	return &OAuthService{
		deps:  deps,
		creds: creds,
		http:  client,
		// Through the web origin by default: the Next proxy attaches the API
		// token, and the provider only ever sees one public URL.
		redirectURL: envOrDefault("OAUTH_REDIRECT_URL", strings.TrimRight(app, "/")+"/api/oauth/callback"),
		appBaseURL:  app,
		states:      map[string]oauthState{},
	}
}

// Status reports whether credType supports sign-in and whether its OAuth app
// is configured.
func (s *OAuthService) Status(credType string) OAuthAppStatus {
	st := OAuthAppStatus{Type: credType, RedirectURL: s.redirectURL}
	if _, ok := s.provider(credType); !ok {
		return st
	}
	st.Supported = true
	idEnv, secEnv := oauthEnvNames(credType)
	st.ClientIDEnv, st.ClientSecEnv = idEnv, secEnv
	id, sec := oauthClient(credType)
	st.Configured = id != "" && sec != ""
	return st
}

// StartURL validates the request, remembers the state, and returns the
// provider consent URL. props fill {placeholders} and are stored with the token.
func (s *OAuthService) StartURL(credType, name, credentialID string, props map[string]any) (string, error) {
	p, ok := s.provider(credType)
	if !ok {
		return "", domain.BadRequest("credential type '" + credType + "' does not support sign-in")
	}
	clientID, clientSecret := oauthClient(credType)
	if clientID == "" || clientSecret == "" {
		idEnv, secEnv := oauthEnvNames(credType)
		return "", domain.Errorf(domain.CodeValidationFailed, "sign-in is not configured for this app — set %s and %s on the API", idEnv, secEnv)
	}
	if credentialID != "" {
		existing, ok := s.deps.Repos.Credentials.GetByID(domain.CredentialId(credentialID))
		if !ok || existing.WorkspaceID != s.deps.Repos.Workspace.Get().ID || existing.Type != credType {
			return "", domain.NotFound("Credential")
		}
	}

	authURL, err := url.Parse(fillPlaceholders(p.AuthURL, props))
	if err != nil || authURL.Host == "" || strings.Contains(authURL.Host, "{") {
		return "", domain.BadRequest("fill in the connection fields before signing in")
	}
	scopes := make([]string, 0, len(p.Scope))
	for _, sc := range p.Scope {
		if v := strings.TrimSpace(fillPlaceholders(sc, props)); v != "" {
			scopes = append(scopes, v)
		}
	}

	state := randToken()
	st := oauthState{
		credType: credType, name: name, credentialID: credentialID, props: props,
		tokenURL: fillPlaceholders(p.TokenURL, props), issued: time.Now(),
	}
	q := authURL.Query() // some providers (Slack) ship extra params in authUrl
	q.Set("client_id", clientID)
	q.Set("redirect_uri", s.redirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	if p.PKCE {
		st.verifier = randToken() + randToken()
		sum := sha256.Sum256([]byte(st.verifier))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range p.Extra {
		// AP's camelCase grantType/responseType are framework hints, not params.
		if k != "grantType" && k != "responseType" && v != "" {
			q.Set(k, v)
		}
	}
	if authURL.Host == "accounts.google.com" {
		// Without these Google returns no refresh token and the login dies in an hour.
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
	}
	authURL.RawQuery = q.Encode()

	s.mu.Lock()
	for k, v := range s.states {
		if time.Since(v.issued) > oauthStateTTL {
			delete(s.states, k)
		}
	}
	s.states[state] = st
	s.mu.Unlock()
	return authURL.String(), nil
}

// HandleCallback exchanges the code and persists the credential, returning its id.
func (s *OAuthService) HandleCallback(ctx context.Context, code, state, providerErr string) (CredentialSummary, error) {
	s.mu.Lock()
	st, ok := s.states[state]
	delete(s.states, state)
	s.mu.Unlock()
	if !ok || time.Since(st.issued) > oauthStateTTL {
		return CredentialSummary{}, domain.Errorf(domain.CodeValidationFailed, "sign-in expired or was already used — try again")
	}
	if providerErr != "" {
		return CredentialSummary{}, domain.Errorf(domain.CodeValidationFailed, "the provider declined the sign-in (%s)", providerErr)
	}
	if code == "" {
		return CredentialSummary{}, domain.Errorf(domain.CodeValidationFailed, "missing authorization code")
	}
	p, _ := s.provider(st.credType)
	clientID, clientSecret := oauthClient(st.credType)

	tok, err := s.exchange(ctx, st, p, clientID, clientSecret, code)
	if err != nil {
		return CredentialSummary{}, err
	}
	access, _ := tok["access_token"].(string)
	if access == "" {
		// Slack v2 user-token installs nest it under authed_user.
		if au, ok := tok["authed_user"].(map[string]any); ok {
			access, _ = au["access_token"].(string)
		}
	}
	if access == "" {
		return CredentialSummary{}, domain.Errorf(domain.CodeInternal, "the provider returned no access token")
	}

	values := map[string]any{
		"access_token": access,
		"token_type":   orStr(strVal(tok["token_type"]), "Bearer"),
		"token_url":    st.tokenURL,
		"data":         tok, // pieces read provider extras (team, authed_user…) from auth.data
	}
	if _, key := oauthLink(st.credType); key != "" {
		values[key] = access
		values["token_key"] = key // the reasoner's refresh writes the new token here too
	}
	if rt := strVal(tok["refresh_token"]); rt != "" {
		values["refresh_token"] = rt
	}
	if p.AuthorizationMethod == "header" {
		values["authorization_method"] = "header" // the worker's refresh call needs it
	}
	if sc := strVal(tok["scope"]); sc != "" {
		values["scope"] = sc
	}
	if exp := numVal(tok["expires_in"]); exp > 0 {
		values["expires_at"] = time.Now().Unix() + int64(exp)
	}
	if len(st.props) > 0 {
		values["props"] = st.props
		for k, v := range st.props {
			values[k] = v
		}
	}

	if st.credentialID != "" {
		return s.creds.Update(domain.CredentialId(st.credentialID), nilIfEmpty(st.name), values)
	}
	name := st.name
	if strings.TrimSpace(name) == "" {
		name = credentialLabel(st.credType) + " account"
	}
	return s.creds.Create(name, st.credType, values)
}

// DoneURL is the web page the popup lands on after the callback.
func (s *OAuthService) DoneURL(status, credentialID, message string) string {
	q := url.Values{"status": {status}}
	if credentialID != "" {
		q.Set("credentialId", credentialID)
	}
	if message != "" {
		q.Set("message", message)
	}
	return strings.TrimRight(s.appBaseURL, "/") + "/oauth/done?" + q.Encode()
}

/* ----------------------------- internals ----------------------------- */

func (s *OAuthService) exchange(ctx context.Context, st oauthState, p PieceOAuth, clientID, clientSecret, code string) (map[string]any, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {s.redirectURL},
	}
	if st.verifier != "" {
		form.Set("code_verifier", st.verifier)
	}
	header := p.AuthorizationMethod == "header"
	if !header {
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, st.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, domain.Errorf(domain.CodeInternal, "bad token URL: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if header {
		req.SetBasicAuth(clientID, clientSecret)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, domain.Errorf(domain.CodeInternal, "token exchange failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		s.deps.Logger.Error("oauth token exchange failed", "type", st.credType, "status", resp.StatusCode, "body", truncateStr(string(body), 300))
		return nil, domain.Errorf(domain.CodeInternal, "token exchange failed (HTTP %d)", resp.StatusCode)
	}
	var tok map[string]any
	if json.Unmarshal(body, &tok) != nil {
		// A few providers (GitHub-style) answer form-encoded despite Accept.
		vals, perr := url.ParseQuery(string(body))
		if perr != nil {
			return nil, domain.Errorf(domain.CodeInternal, "unreadable token response")
		}
		tok = map[string]any{}
		for k := range vals {
			tok[k] = vals.Get(k)
		}
	}
	if ok, present := tok["ok"].(bool); present && !ok { // Slack reports errors in a 200
		return nil, domain.Errorf(domain.CodeInternal, "token exchange failed: %v", tok["error"])
	}
	return tok, nil
}

func (s *OAuthService) provider(credType string) (PieceOAuth, bool) {
	s.loadOnce.Do(func() { s.providers = loadOAuthProviders(piecesIndexPath()) })
	via, _ := oauthLink(credType)
	p, ok := s.providers[via]
	return p, ok
}

// oauthLink maps a credential type to the piece type whose OAuth flow it uses
// and the field the access token is stored under. Piece types use their own
// flow and keep the token in access_token; linked catalog types (slack →
// pieces.slack) store it under their env-style key, e.g. SLACK_BOT_TOKEN.
// The env names for the client id/secret are the same either way
// (oauthEnvNames strips "pieces."), so one registered app serves both.
func oauthLink(credType string) (via, tokenKey string) {
	if types, _ := credentialTypes(); types != nil {
		if def, ok := types[credType]; ok && def.OAuthVia != "" {
			return def.OAuthVia, def.OAuthTokenKey
		}
	}
	return credType, ""
}

// loadOAuthProviders reads auth.oauth for every piece in the index, keyed by
// credential type ("pieces.<slug>").
func loadOAuthProviders(path string) map[string]PieceOAuth {
	out := map[string]PieceOAuth{}
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var idx struct {
		Nodes []struct {
			ID   string `json:"id"`
			Auth *struct {
				OAuth *PieceOAuth `json:"oauth"`
			} `json:"auth"`
		} `json:"nodes"`
	}
	if json.Unmarshal(raw, &idx) != nil {
		return out
	}
	for _, n := range idx.Nodes {
		if n.Auth == nil || n.Auth.OAuth == nil {
			continue
		}
		typ := "pieces." + pieceSlugOf(n.ID)
		if _, seen := out[typ]; !seen {
			out[typ] = *n.Auth.OAuth
		}
	}
	return out
}

// oauthEnvNames: pieces.google-sheets → OAUTH_GOOGLE_SHEETS_CLIENT_ID/SECRET.
func oauthEnvNames(credType string) (string, string) {
	slug := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(strings.TrimPrefix(credType, "pieces.")))
	return "OAUTH_" + slug + "_CLIENT_ID", "OAUTH_" + slug + "_CLIENT_SECRET"
}

func oauthClient(credType string) (string, string) {
	idEnv, secEnv := oauthEnvNames(credType)
	id, sec := os.Getenv(idEnv), os.Getenv(secEnv)
	if id == "" || sec == "" {
		// One Google / Microsoft app covers every product in the family.
		id, sec = familyClient(strings.TrimPrefix(credType, "pieces."))
	}
	return id, sec
}

func fillPlaceholders(s string, props map[string]any) string {
	for k, v := range props {
		s = strings.ReplaceAll(s, "{"+k+"}", strVal(v))
	}
	return s
}

func credentialLabel(credType string) string {
	if types, _ := credentialTypes(); types != nil {
		if def, ok := types[credType]; ok && def.Label != "" {
			return def.Label
		}
	}
	return pieceDisplayName(strings.TrimPrefix(credType, "pieces."))
}

func strVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return strings.Trim(string(b), `"`)
	}
}

func numVal(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		var f float64
		_ = json.Unmarshal([]byte(t), &f)
		return f
	}
	return 0
}

func nilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
