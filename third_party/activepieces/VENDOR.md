# Vendored: Activepieces (MIT portions only)

This directory contains a **stripped fork** of Activepieces, vendored into Agently
for the workflow engine, flow builder (graph editor), pieces (node) framework, and
agent layers.

## Provenance

| | |
|---|---|
| Upstream | https://github.com/activepieces/activepieces |
| Pinned tag | `0.85.2` |
| Pinned commit | `0b0c1f120480ebf0a3be07ff0f27cf6dc9e4d4ac` |
| Vendored on | 2026-06-10 |
| License of this tree | MIT Expat (see `LICENSE` in this directory) |

## What was removed (commercially licensed, NOT MIT)

Per the upstream `LICENSE`, two directories are licensed under the Activepieces
commercial/enterprise license and were **deleted entirely** from this fork:

- `packages/ee/` (enterprise auth, billing UI, embed-sdk, and the EE LICENSE)
- `packages/server/api/src/app/ee/` (enterprise server modules: SSO/OTP auth,
  audit logs, API keys, project members/roles/plans, git sync, signing keys,
  platform plans, embed subdomains, secret managers, AppSumo, etc.)

Nothing else was removed for licensing reasons. Note: `packages/shared/src/lib/ee/`
**remains** — it is *outside* the two restricted directories and is therefore MIT
(it contains only shared type/DTO definitions whose names mention "ee").

`.git/` metadata was also removed (this is a snapshot vendor, not a submodule).

## Known consequences of the strip

The upstream codebase ships EE code alongside MIT code and gates it at runtime by
edition (`ApEdition`). Because we deleted the EE directories, **38 files under
`packages/server/api/src/` contain now-dangling imports of `./ee/...` /
`../ee/...` paths** (notably `app/app.ts` and `app/database/database-connection.ts`).
The full API server therefore does NOT compile as-is.

The components Agently builds on are unaffected and have no restricted-path imports:

- `packages/server/engine/` — flow execution engine (clean)
- `packages/server/worker/` — worker runtime (one dangling type import in
  `lib/execute/job-registry.ts`; `packages/shared/.../job-data.ts` has one import
  of `../../ee/audit-events` which resolves to the MIT `shared/src/lib/ee/` copy — fine)
- `packages/shared/` — domain model: flows, triggers, actions (clean)
- `packages/web/` — React flow builder / graph editor (clean; EE features are
  runtime-flag-gated, not import-coupled)
- `packages/pieces/` — the integration ("node") framework + community pieces (clean)
- `packages/cli/` — piece scaffolding CLI (clean)

If you later need `packages/server/api/`, prune the EE import sites (replace the
EE module registrations in `app.ts` and the EE entities in
`database-connection.ts` with community-edition equivalents) rather than
restoring the EE code.

## Upgrade policy (deliberate, pinned)

The upstream open-core line can move between releases (features may migrate into
`ee/`). When upgrading this vendor:

1. Pick a new release tag; never track `main`.
2. **Re-read the upstream `LICENSE` first** — confirm the restricted-directory
   list hasn't grown. If it has, strip the new restricted paths too.
3. Diff `packages/` layout between the old and new tag for moved/renamed
   directories before re-vendoring.
4. Re-run the dangling-import scan:
   `grep -rln "from '\.\{1,2\}/.*ee/" packages/server/api/src`
5. Update this file (tag, commit, date, removal list).

## License obligations

The MIT license requires that the copyright notice and permission notice be
included in all copies or substantial portions of the Software. The original
`LICENSE` file is preserved at the root of this directory — **do not delete it**,
and retain it (or its notice text) in any distribution of Agently that includes
code from this tree. See also `../NOTICE.md` at the Agently repo level.
