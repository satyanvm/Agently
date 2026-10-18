package services

// oauth_apps.go is the operator's checklist for one-click sign-in: the most
// used providers, whether each one's OAuth app is configured, and where to
// register it. GET /api/oauth/apps → Settings → Sign-in apps.
//
// Registering one app per provider and sharing it with every user is the
// "hosted shared app" model n8n Cloud uses: users click "Sign in with X" and
// never see a client id. Google and Microsoft each need just ONE app for their
// whole family (GOOGLE_CLIENT_ID / MICROSOFT_CLIENT_ID, see oauthClient).

import (
	"os"
	"sort"
	"strings"
)

// PopularOAuthApp is one row of the checklist.
type PopularOAuthApp struct {
	OAuthAppStatus
	Label      string `json:"label"`
	ConsoleURL string `json:"consoleUrl"`
	// Family apps share client creds ("google", "microsoft"); empty otherwise.
	Family string `json:"family,omitempty"`
	// Shared env names a family app may use instead of the per-app pair.
	FamilyIDEnv     string `json:"familyClientIdEnv,omitempty"`
	FamilySecretEnv string `json:"familyClientSecretEnv,omitempty"`
}

// popularOAuthApps: credential type → developer console. Catalog types
// (slack, github, …) sign in through their linked piece, so one entry covers
// both node flavours.
var popularOAuthApps = []struct{ typ, console string }{
	{"slack", "https://api.slack.com/apps"},
	{"pieces.gmail", "https://console.cloud.google.com/apis/credentials"},
	{"pieces.google-sheets", "https://console.cloud.google.com/apis/credentials"},
	{"pieces.google-drive", "https://console.cloud.google.com/apis/credentials"},
	{"pieces.google-calendar", "https://console.cloud.google.com/apis/credentials"},
	{"pieces.google-docs", "https://console.cloud.google.com/apis/credentials"},
	{"pieces.google-forms", "https://console.cloud.google.com/apis/credentials"},
	{"pieces.youtube", "https://console.cloud.google.com/apis/credentials"},
	{"github", "https://github.com/settings/developers"},
	{"pieces.gitlab", "https://gitlab.com/-/user_settings/applications"},
	{"notion", "https://www.notion.so/profile/integrations"},
	{"hubspot", "https://developers.hubspot.com/"},
	{"pieces.salesforce", "https://login.salesforce.com/lightning/setup/NavigationMenus/home"},
	{"pieces.microsoft-outlook", "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps"},
	{"pieces.microsoft-teams", "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps"},
	{"pieces.microsoft-excel-365", "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps"},
	{"pieces.microsoft-onedrive", "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps"},
	{"pieces.linkedin", "https://www.linkedin.com/developers/apps"},
	{"asana", "https://app.asana.com/0/my-apps"},
	{"clickup", "https://app.clickup.com/settings/apps"},
	{"dropbox", "https://www.dropbox.com/developers/apps"},
	{"pieces.box", "https://app.box.com/developers/console"},
	{"pieces.zoom", "https://marketplace.zoom.us/develop/create"},
	{"intercom", "https://app.intercom.com/a/apps/_/developer-hub"},
	{"typeform", "https://admin.typeform.com/account#/section/apps"},
	{"todoist", "https://developer.todoist.com/appconsole.html"},
	{"pieces.mailchimp", "https://admin.mailchimp.com/account/oauth2/"},
	{"pieces.pipedrive", "https://developers.pipedrive.com/"},
	{"pieces.quickbooks", "https://developer.intuit.com/app/developer/dashboard"},
	{"pieces.xero", "https://developer.xero.com/app/manage"},
	{"pieces.figma", "https://www.figma.com/developers/apps"},
}

// PopularApps returns the checklist (supported providers only), unconfigured
// first so the to-do list reads top-down.
func (s *OAuthService) PopularApps() []PopularOAuthApp {
	out := make([]PopularOAuthApp, 0, len(popularOAuthApps))
	for _, a := range popularOAuthApps {
		st := s.Status(a.typ)
		if !st.Supported {
			continue
		}
		row := PopularOAuthApp{OAuthAppStatus: st, Label: credentialLabel(a.typ), ConsoleURL: a.console}
		if fam, idEnv, secEnv := oauthFamily(strings.TrimPrefix(a.typ, "pieces.")); fam != "" {
			row.Family, row.FamilyIDEnv, row.FamilySecretEnv = fam, idEnv, secEnv
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return !out[i].Configured && out[j].Configured })
	return out
}

// oauthFamily: providers whose apps cover many products with one client.
func oauthFamily(slug string) (family, idEnv, secretEnv string) {
	switch {
	case strings.HasPrefix(slug, "google") || slug == "gmail" || slug == "youtube":
		return "google", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"
	case strings.HasPrefix(slug, "microsoft"):
		return "microsoft", "MICROSOFT_CLIENT_ID", "MICROSOFT_CLIENT_SECRET"
	}
	return "", "", ""
}

func familyClient(slug string) (string, string) {
	if _, idEnv, secEnv := oauthFamily(slug); idEnv != "" {
		return os.Getenv(idEnv), os.Getenv(secEnv)
	}
	return "", ""
}
