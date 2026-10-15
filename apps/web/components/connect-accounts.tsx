"use client";

import * as React from "react";
import { KeyRound, Loader2, Server } from "lucide-react";
import { Button } from "@/components/ui/button";
import { CredentialModal } from "@/components/builder/credential-section";
import { credentialTypeDef } from "@/components/builder/credential-types";
import { listCredentials, type CredentialSummary } from "@/lib/credentials";
import { applyWorkflowCredential, type PreflightAccount, type RunPreflight } from "@/lib/api";

/**
 * "Connect these accounts" — the accounts a workflow's nodes still need before a
 * run can start (GET /api/workflows/{slug}/preflight). Each account can reuse a
 * stored credential or create one; either way the API selects it on every node
 * that needs it in one step, and the parent receives the refreshed preflight.
 * Accounts with no credential type are server settings: only the operator can
 * set those env vars, so they are listed, not editable.
 */
export function ConnectAccounts({
  slug,
  preflight,
  onChange,
}: {
  slug: string;
  preflight: RunPreflight;
  onChange: (next: RunPreflight) => void | Promise<void>;
}) {
  const [stored, setStored] = React.useState<CredentialSummary[]>([]);
  React.useEffect(() => {
    listCredentials().then(setStored).catch(() => setStored([]));
  }, []);

  if (preflight.ready) return null;
  return (
    <div className="space-y-2">
      <p className="text-[12px] text-muted">
        Connect these accounts first. Without them the run would stop at the first node that needs one.
      </p>
      {preflight.accounts.map((a) => (
        <AccountRow
          key={a.credentialType || a.envKeys.join(",")}
          slug={slug}
          account={a}
          stored={stored.filter((c) => c.type === a.credentialType)}
          onStored={(c) => setStored((prev) => [...prev.filter((p) => p.id !== c.id), c])}
          onChange={onChange}
        />
      ))}
    </div>
  );
}

function AccountRow({
  slug,
  account,
  stored,
  onStored,
  onChange,
}: {
  slug: string;
  account: PreflightAccount;
  stored: CredentialSummary[];
  onStored: (c: CredentialSummary) => void;
  onChange: (next: RunPreflight) => void | Promise<void>;
}) {
  const [creating, setCreating] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const serverSetting = account.credentialType === "";

  const apply = async (id: string) => {
    setBusy(true);
    setError(null);
    try {
      await onChange(await applyWorkflowCredential(slug, id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not connect the account");
    } finally {
      setBusy(false);
    }
  };

  const nodeNames = account.nodes.map((n) => n.name).join(", ");
  const problem = account.nodes.some((n) => n.problem === "credential_deleted")
    ? "The selected credential was deleted."
    : account.nodes.some((n) => n.problem === "incomplete")
      ? `The selected credential is missing ${[...new Set(account.nodes.flatMap((n) => n.missingKeys ?? []))].join(", ")}.`
      : null;

  return (
    <div className="rounded-md border border-border px-3 py-2.5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-[13px] font-medium text-fg">
            {serverSetting ? <Server className="size-3.5 text-faint" /> : <KeyRound className="size-3.5 text-faint" />}
            {account.label}
          </div>
          <div className="mt-0.5 truncate text-[11px] text-faint" title={nodeNames}>
            Used by {account.nodes.length === 1 ? "" : `${account.nodes.length} nodes: `}
            {nodeNames}
          </div>
          {problem && <div className="mt-0.5 text-[11px] text-warn">{problem}</div>}
          {serverSetting && (
            <div className="mt-0.5 text-[11px] text-muted">
              Set <code className="font-mono">{account.envKeys.join(", ")}</code> in the server&apos;s .env and restart.
            </div>
          )}
        </div>
        {!serverSetting && (
          <div className="flex shrink-0 items-center gap-1.5">
            {stored.length > 0 && (
              <select
                className="h-7 max-w-[150px] rounded-md border border-border bg-surface px-1.5 text-[12px]"
                disabled={busy}
                value=""
                onChange={(e) => e.target.value && apply(e.target.value)}
              >
                <option value="">Use saved…</option>
                {stored.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            )}
            <Button variant="secondary" size="sm" disabled={busy} onClick={() => setCreating(true)}>
              {busy ? <Loader2 className="size-3.5 animate-spin" /> : null}
              Connect
            </Button>
          </div>
        )}
      </div>
      {error && <p className="mt-1 break-words text-[11px] text-danger">{error}</p>}
      {creating && (
        <CredentialModal
          typeDef={credentialTypeDef(account.credentialType)}
          onClose={() => setCreating(false)}
          onSaved={async (cred) => {
            setCreating(false);
            onStored(cred);
            await apply(cred.id);
          }}
          onDeleted={() => setCreating(false)}
        />
      )}
    </div>
  );
}
