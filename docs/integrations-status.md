# Publishing integrations: status

*Checked 2026-10-18 on the local stack (step 9 of `docs/phase-2-plan.md`).
"Live tested" stays **no** until a real account is connected and a real action
runs. This file is updated as that happens.*

## What was checked

1. **Load:** every package is loaded with the pieces worker's own registry
   (`apps/pieces-worker/dist/pieces.js`). All 707 installed packages load; none fail.
2. **Inputs and sign-in:** each action's required inputs and the auth it
   declares, read from the loaded piece.
3. **No-credential behaviour:** run straight through the worker with no auth,
   each action fails inside the piece with an unhelpful error ("Cannot read
   properties of undefined"). That can't happen in a real run: the reasoner's
   `prepare_piece_node` stops a piece step with no credential first and names
   the missing credential.
4. **Sign-in apps and accounts:** `GET /api/oauth/apps` and
   `GET /api/credentials`. Nothing is configured or connected yet.

## Status

| Integration | Action(s) used by playbooks | Loads | Sign-in | Required inputs | Configured | Live tested | Notes |
|---|---|---|---|---|---|---|---|
| Google Docs | `create_document`, `append_text` | yes | OAuth (Google) | title + body; text + documentId | no | no | One Google OAuth app (`GOOGLE_CLIENT_ID/SECRET`) covers Docs and Gmail. Needs the Docs and Drive APIs enabled in Google Cloud. |
| Gmail | `send_email` | yes | OAuth (Google) | receiver, subject, body_type, body, draft | no | no | `draft: true` saves a Gmail draft instead of sending: the safe default for customer emails. Sending scopes may need Google's app verification beyond test users. |
| Slack (piece) | `send_channel_message`, `send_direct_message` | yes | OAuth (Slack) | channel + sendAsBot; userId + text | no | no | The piece asks for about 30 scopes; the Slack app has to list all of them. `request_approval_message` needs pause/resume, which the worker doesn't support, so the Inbox replaces it. |
| Slack (simple) | `slack.sendMessage` (catalog), `output.slack` (webhook) | yes | bot token / webhook URL | channel + text / webhook URL | no | no | Simplest route for team notifications and approval alerts: an incoming webhook needs no OAuth app at all. |
| LinkedIn | `create_share_update` (personal), `create_company_update` (company page) | yes | OAuth (LinkedIn) | text + visibility; company + text | no | no | The piece requests organization scopes (`w_organization_social`, `rw_organization_admin`) that LinkedIn grants only to apps approved for its Community Management API, and LinkedIn rejects the whole sign-in otherwise. **Fix shipped:** set `OAUTH_LINKEDIN_SCOPES=openid profile email w_member_social` to sign in with the self-serve "Share on LinkedIn" product. Personal posts then work; company-page posts still need LinkedIn's approval. |
| X | `create-tweet` | yes | keys pasted (OAuth 1.0a user tokens) | text | no | no | No sign-in popup: consumer key/secret plus access token/secret from the X developer portal, with Read and Write permission. Whether your X API plan allows posting, and how much, depends on the plan. Check it before relying on X. |

## What a real test needs from you

1. **Google (Docs + Gmail):**
   - In Google Cloud, create an OAuth client (type: Web application) with
     redirect URL `http://localhost:3000/api/oauth/callback`.
   - Enable the Google Docs, Drive and Gmail APIs.
   - Add your Google account as a test user.
   - Set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`.
2. **Slack:** either
   - an incoming webhook URL for a test channel, which is enough for approval
     notifications and `output.slack`; or
   - a Slack app with the piece's scopes and the same redirect URL, plus
     `OAUTH_SLACK_CLIENT_ID/SECRET`.
3. **LinkedIn:**
   - Create a LinkedIn developer app with the "Share on LinkedIn" and
     "Sign In with LinkedIn using OpenID Connect" products, and the same
     redirect URL.
   - Set `OAUTH_LINKEDIN_CLIENT_ID/SECRET` and the `OAUTH_LINKEDIN_SCOPES`
     line above.
4. **X:** a developer app with Read and Write permission, and its four keys.
5. **Then:** restart the API (`pnpm agently:api`) and sign in from
   **Settings → Sign-in apps** (or the step's "Sign in" button in the builder).

Signing in and pasting keys is done by you; Agently stores the result
encrypted. With accounts connected, the live tests are:
- Google Docs: create a private test doc.
- Gmail: create a draft, nothing sent.
- Slack: post to a test channel you choose.
- LinkedIn and X: post publicly, only if you say so.
