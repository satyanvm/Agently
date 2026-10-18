package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

func TestOAuthStartAndCallbackStoresCredential(t *testing.T) {
	var gotForm url.Values
	var gotBasic bool
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _, gotBasic = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600, "token_type": "Bearer",
		})
	}))
	defer tokenSrv.Close()

	t.Setenv("OAUTH_ACME_CLIENT_ID", "cid")
	t.Setenv("OAUTH_ACME_CLIENT_SECRET", "csecret")
	p := newTestPlatform()
	svc := p.OAuth
	svc.loadOnce.Do(func() {}) // inject the provider instead of reading the index
	svc.providers = map[string]PieceOAuth{"pieces.acme": {
		AuthURL:  "https://{sub}.acme.test/authorize?fixed=1",
		TokenURL: tokenSrv.URL + "/{sub}/token",
		Scope:    []string{"read", "{extraScope}"},
		PKCE:     true,
	}}
	// Register the fake type without clobbering the shared catalog for other tests.
	credentialTypes()
	prev := credTypesVal
	withAcme := map[string]CredentialTypeDef{"pieces.acme": {ID: "pieces.acme", Label: "Acme"}}
	for k, v := range prev {
		withAcme[k] = v
	}
	credTypesVal = withAcme
	defer func() { credTypesVal = prev }()

	if st := svc.Status("pieces.acme"); !st.Supported || !st.Configured || st.ClientIDEnv != "OAUTH_ACME_CLIENT_ID" {
		t.Fatalf("status = %+v", st)
	}
	if st := svc.Status("pieces.nope"); st.Supported {
		t.Fatal("unknown type should be unsupported")
	}

	props := map[string]any{"sub": "team", "extraScope": "write"}
	raw, err := svc.StartURL("pieces.acme", "My Acme", "", props)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if u.Host != "team.acme.test" || q.Get("fixed") != "1" || q.Get("scope") != "read write" ||
		q.Get("client_id") != "cid" || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" {
		t.Fatalf("bad auth url %s", raw)
	}

	cred, err := svc.HandleCallback(context.Background(), "the-code", q.Get("state"), "")
	if err != nil {
		t.Fatal(err)
	}
	if gotForm.Get("code") != "the-code" || gotForm.Get("client_secret") != "csecret" || gotForm.Get("code_verifier") == "" || gotBasic {
		t.Fatalf("bad token request %v basic=%v", gotForm, gotBasic)
	}
	if cred.Name != "My Acme" || cred.Type != "pieces.acme" {
		t.Fatalf("cred = %+v", cred)
	}
	stored, _ := p.Repos.Credentials.GetByID(cred.ID)
	if stored.Data["access_token"] != "at-1" || stored.Data["refresh_token"] != "rt-1" ||
		stored.Data["sub"] != "team" || !strings.HasSuffix(stored.Data["token_url"].(string), "/team/token") ||
		stored.Data["expires_at"] == nil {
		t.Fatalf("stored = %#v", stored.Data)
	}

	// States are single-use.
	if _, err := svc.HandleCallback(context.Background(), "the-code", q.Get("state"), ""); err == nil {
		t.Fatal("replayed state should fail")
	}
	// Re-authorizing an existing credential updates it in place.
	raw2, _ := svc.StartURL("pieces.acme", "", string(cred.ID), props)
	u2, _ := url.Parse(raw2)
	again, err := svc.HandleCallback(context.Background(), "c2", u2.Query().Get("state"), "")
	if err != nil || again.ID != cred.ID {
		t.Fatalf("reauth = %+v, %v", again, err)
	}
	if _, err := svc.StartURL("pieces.acme", "", string(domain.NewCredentialId()), props); err == nil {
		t.Fatal("unknown credential id should fail")
	}
}

func TestOAuthStartRequiresClientConfig(t *testing.T) {
	p := newTestPlatform()
	p.OAuth.loadOnce.Do(func() {})
	p.OAuth.providers = map[string]PieceOAuth{"pieces.zzz": {AuthURL: "https://x.test/a", TokenURL: "https://x.test/t"}}
	_, err := p.OAuth.StartURL("pieces.zzz", "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "OAUTH_ZZZ_CLIENT_ID") {
		t.Fatalf("want config error naming the env var, got %v", err)
	}
}

func TestOAuthCatalogTypeSignsInThroughLinkedPiece(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxb-1","token_type":"bot"}`))
	}))
	defer tokenSrv.Close()

	t.Setenv("OAUTH_ACMECHAT_CLIENT_ID", "cid")
	t.Setenv("OAUTH_ACMECHAT_CLIENT_SECRET", "sec")
	p := newTestPlatform()
	svc := p.OAuth
	svc.loadOnce.Do(func() {})
	svc.providers = map[string]PieceOAuth{"pieces.acmechat": {AuthURL: "https://acme.test/a", TokenURL: tokenSrv.URL}}
	credentialTypes()
	prev := credTypesVal
	next := map[string]CredentialTypeDef{"acmechat": {
		ID: "acmechat", Label: "AcmeChat", Source: "catalog",
		OAuthVia: "pieces.acmechat", OAuthTokenKey: "ACMECHAT_BOT_TOKEN",
	}}
	for k, v := range prev {
		next[k] = v
	}
	credTypesVal = next
	defer func() { credTypesVal = prev }()

	if st := svc.Status("acmechat"); !st.Supported || !st.Configured || st.ClientIDEnv != "OAUTH_ACMECHAT_CLIENT_ID" {
		t.Fatalf("status = %+v", st)
	}
	raw, err := svc.StartURL("acmechat", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	cred, err := svc.HandleCallback(context.Background(), "c", u.Query().Get("state"), "")
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := p.Repos.Credentials.GetByID(cred.ID)
	if cred.Type != "acmechat" || stored.Data["ACMECHAT_BOT_TOKEN"] != "xoxb-1" || stored.Data["token_key"] != "ACMECHAT_BOT_TOKEN" {
		t.Fatalf("cred=%+v stored=%#v", cred, stored.Data)
	}
}

func TestOAuthFamilyFallbackAndPopularApps(t *testing.T) {
	t.Setenv("MICROSOFT_CLIENT_ID", "ms")
	t.Setenv("MICROSOFT_CLIENT_SECRET", "ms-sec")
	if id, sec := oauthClient("pieces.microsoft-teams"); id != "ms" || sec != "ms-sec" {
		t.Fatalf("microsoft family fallback: %q %q", id, sec)
	}
	if id, _ := oauthClient("pieces.slack"); id != "" && id == "ms" {
		t.Fatal("non-family app must not borrow the Microsoft client")
	}
	p := newTestPlatform()
	apps := p.OAuth.PopularApps()
	if len(apps) == 0 {
		t.Skip("no pieces index in this checkout")
	}
	seenConfigured := false
	for _, a := range apps {
		if a.Configured {
			seenConfigured = true
		} else if seenConfigured {
			t.Fatal("unconfigured apps must sort first")
		}
		if a.Type == "pieces.microsoft-teams" && (!a.Configured || a.Family != "microsoft") {
			t.Fatalf("teams row = %+v", a)
		}
	}
}
