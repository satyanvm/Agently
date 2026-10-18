# Agently — product

## Problem

Small B2B software marketing teams find out about competitor moves too late,
and then react too slowly. A competitor cuts prices, ships a feature or starts
hiring for a new market. Someone notices days later, often from a sales call.
Turning that into a battlecard update, a blog post or a customer email takes
another week of meetings and drafts.

The tools that solve this are built for enterprises. Intelligence suites (Klue,
Crayon, Meltwater) tell you what happened but stop there, and they are
quote-only annual contracts. Execution tools (Jasper, HubSpot Breeze, Typeface)
write content but don't know what your competitors just did. A team of three
can't afford either kind, let alone both, and can't stitch them together.

## Target user (first version)

B2B software companies with **one to five marketers** and no budget for
enterprise intelligence tools. The buyer is usually the head of marketing or
product marketing, who also writes the battlecards, the comparison pages and
half of the social posts.

## Core loop

```text
signal → meaning → draft → human approval → publish → record
```

1. **Signal.** Agently watches sources it collects itself: competitor pricing,
   changelog, blog and careers pages; GDELT news; RSS; Reddit and Hacker News;
   review sites. It doesn't use paid intelligence vendors.
2. **Meaning.** An AI step decides what the change means for us. For example:
   "they're now cheaper on the starter tier", "they shipped something we
   already do better", or "they shipped something we're missing".
3. **Draft.** AI drafts the response in the workspace's brand voice, using its
   product facts and avoiding its banned claims: a battlecard update, a
   comparison-page edit, a blog post, a LinkedIn post, a sales talk track, a
   customer email.
4. **Approval.** A person edits, approves or rejects each draft. Nothing
   publishes without approval. The only exception is a short list of
   explicitly safe actions, such as pausing scheduled posts.
5. **Publish.** Approved drafts go out through real integrations, such as
   Google Docs, Notion, HubSpot, WordPress, Webflow, LinkedIn, X, Buffer, Slack
   and Gmail.
6. **Record.** Every published item is traceable: the signal that caused it,
   the run that drafted it, what that run cost, who approved it and when it
   went out.

Users describe their own playbooks in plain English, and the planner compiles
them into editable workflow graphs. Most users start from a ready-made
playbook.

## First-version scope

- **Competitor registry and snapshots.** Register competitors with their
  pricing, changelog, blog and careers URLs. Snapshot those pages on a
  schedule, store the snapshots in Postgres, and compute what changed since the
  last snapshot. This is exposed as planner nodes ("snapshot page", "diff
  against last snapshot").
- **Human approval node.** A node that pauses the run durably until someone
  approves, edits or rejects the draft, or until a timeout. It sends a Slack or
  email notification with a link.
- **Approval inbox.** Pending drafts, each shown with the signal that caused
  it. Users can edit, approve or reject, and see the full history (signal →
  run → draft → approver → published item).
- **Brand context.** A per-workspace store of brand voice, product facts,
  positioning and claims to avoid, injected into the AI drafting nodes.
- **Three ready-made playbooks:**
  1. *Competitor pricing changed:* analysis → battlecard update,
     comparison-page draft, at-risk-customer email and Slack talk track →
     approval → publish.
  2. *Competitor launched a feature:* "we're better" leads to drafting a blog
     post, a LinkedIn post and ad copy; "we're missing it" leads to alerting
     the product team and drafting a sales FAQ → approval → publish or notify.
  3. *Weekly content plan:* the week's signals → top five opportunities →
     outlines and social posts → approval → schedule.
- **Planner coverage.** Plain-English marketing playbook prompts route to the
  new nodes, measured by the planner evaluation harness.

## Out of scope for the first version

- Crisis detection with automatic pausing of scheduled posts.
- Measuring how published content performed.
- Multiple users per workspace, and roles.
- Paid intelligence vendors (Meltwater and others) and their data.
- Publishing anything without human approval.
- Mock or simulated output of any kind. A missing credential or integration
  fails the run with the real reason.

## How success is measured

- **Signal to approved draft:** time from a detected change to a draft waiting
  in the inbox (target: same day), and from there to publish.
- **Approval rate:** the share of drafts approved, and how heavily they were
  edited first. This shows whether brand context makes drafts usable.
- **Signal precision:** the share of detected changes that are real and
  meaningful, not layout noise or timestamps.
- **Cost per published item:** model and browser cost, taken from run cost
  tracking.
- **Traceability:** every published item links back to its signal, run and
  approver (target: 100%).
- **Retention:** workspaces with at least one playbook still running after
  four weeks.
