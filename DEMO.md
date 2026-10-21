# Agently demo

The demo shows the whole loop on the **Competitor pricing changed** playbook:

```text
signal → meaning → draft → a person approves → publish → record
```

A competitor raises its prices. Agently notices and works out what it means.
It drafts four pieces in your brand voice, and they wait in the Inbox. You
edit one and approve it, reject the rest, and it goes out. The history then
traces it back to the price change, the run, its cost and you.

It needs a real model (Gemini, or local Ollama), Postgres, Temporal and
Langfuse. Nothing is ever replaced with mock output: a missing dependency fails
the step with its reason, and that visible failure is part of the demo.

## 0. Start

```bash
cp .env.example .env    # fill in the required values
pnpm agently
```

Open http://localhost:3000. Temporal UI is at http://localhost:8080 and
Langfuse at http://localhost:3001.

A real competitor won't change its prices on cue, so the demo uses a stand-in
competitor, "Testco", plus a stand-in Slack webhook, both on your machine:

```bash
python3 scripts/demo_competitor_site.py
```

Leave it running: it prints every Slack message the playbook sends.

## 1. Tell Agently who you are and who you watch

1. **Brand** (sidebar): fill in voice, product facts, positioning and claims
   to avoid. For example: voice "plain, calm, specific; no exclamation
   marks"; facts "Starter $9/month flat, Business $29/month flat with SSO,
   unlimited team members"; avoid "never claim to be the cheapest". Every
   drafting step uses this. A drafting step refuses to run while it's empty.
2. **Competitors → Add competitor:** name `Testco`, one pricing page
   `http://127.0.0.1:18701/pricing`.

## 2. Start from the playbook

**New playbook → Start from a playbook → Competitor pricing changed.** It
lists the sign-ins it uses (Google Docs, Gmail) and whether they're connected.
Fill in:

| Field | Demo value |
|---|---|
| Who approves | your name |
| Battlecard Google Doc ID | any doc ID (only used if you approve the battlecard) |
| At-risk customer emails | `customer@example.com` |
| Sales channel Slack webhook | `http://127.0.0.1:18702/hook` |
| Only these competitors | `Testco` |

Open the created playbook. The graph is a schedule (weekdays 08:00), then:
1. watch Testco's pricing page;
2. continue only if it changed;
3. analyse what it means;
4. four brand-aware drafts, each behind its own approval;
5. a Google Doc, a Gmail draft, or a Slack post.

## 3. First run: the baseline

**Run now.** "Watch competitors" stores Testco's page as the first version.
Nothing changed yet, so the branch is false and every later step is
**skipped**. Nothing is drafted from nothing.

## 4. The competitor raises prices

```bash
python3 scripts/demo_competitor_site.py --raise
```

**Run now** again. This time:

- **The change is detected:** the run finds the new version and records a
  *"Testco pricing page changed"* signal with the lines added and removed.
- **Four drafts appear in the Inbox together**, each showing that signal: a
  battlecard update, a comparison-page update, a customer email and a sales
  talk track.
- **The run is paused** ("Waiting for approval"). It holds no worker while it
  waits.

## 5. Durability, while it waits

Restart the reasoner, which executes the run (`pnpm agently:reasoner`). The
run is still waiting at the same step. Temporal kept its
place, and nothing re-runs or gets drafted twice.

## 6. Decide

Open the **sales talk track** in the Inbox. Check it against the change. AI
drafts can misread a diff: in our run it said *support* moved to Enterprise
when it was *SSO*. Fix the text and add your name. Then **Approve with edits**.

Reject the other three with a note, or keep them for the end of the demo.
Within a second or two the run picks up the decision:

- **Approved talk track:** the Slack stand-in prints the edited text, exactly
  as approved.
- **Rejected drafts:** everything after them is skipped. Nothing goes out
  without approval.

## 7. Trace it

The approval page's **History** reads, oldest first:

1. the run starting;
2. the *Testco pricing page changed* signal;
3. the draft sent to you;
4. "Approved by you (edited)", with your note;
5. the Slack post;
6. the run finishing, with its cost.

The run link opens the full run (each step's input and output, logs), and the
Langfuse link opens every model call.

## 8. Honest failures

Approve the **battlecard** draft without having signed in to Google. The
"Append to the battlecard doc" step fails with *"pieces.google-docs… is
missing credential"*. The run says so; it doesn't pretend the doc was updated.
Sign in under **Settings → Sign-in apps** (see `docs/integrations-status.md`)
and it works.

---

Clean up afterwards: archive Testco on the Competitors page, clear the Brand
page if the values were only for the demo, and stop
`demo_competitor_site.py`.
