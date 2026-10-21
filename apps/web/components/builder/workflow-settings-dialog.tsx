"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { fetchWorkflows } from "@/lib/api";
import {
  fetchWorkflowSettings,
  saveWorkflowSettings,
  type WorkflowSettings,
} from "@/lib/builder-debug";

/**
 * Workflow-level failure handling (apps/api services/failures.go): when a run
 * fails after its nodes' own retries, start an error workflow with the
 * failure as input and/or alert by email and Slack.
 */
export function WorkflowSettingsDialog({ slug, onClose }: { slug: string; onClose: () => void }) {
  const [s, setS] = React.useState<WorkflowSettings | null>(null);
  const [workflows, setWorkflows] = React.useState<{ slug: string; name: string }[]>([]);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    fetchWorkflowSettings(slug).then(setS).catch((e) => setError(String(e?.message ?? e)));
    fetchWorkflows()
      .then((ws) => setWorkflows(ws.filter((w) => w.slug !== slug).map((w) => ({ slug: w.slug, name: w.name }))))
      .catch(() => {});
  }, [slug]);

  const save = async () => {
    if (!s) return;
    setBusy(true);
    setError(null);
    try {
      await saveWorkflowSettings(slug, s);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save settings");
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} className="max-w-[460px]">
      <DialogHeader
        title="When a run fails"
        subtitle="Runs only fail after each node's own retries and continue-on-fail settings are exhausted."
        onClose={onClose}
      />
      <div className="space-y-4 px-5 py-4">
        {!s ? (
          <p className="text-[12px] text-faint">{error ?? "Loading…"}</p>
        ) : (
          <>
            <Field
              label="Error workflow"
              help="Started with {{input.failedRun}} — workflow, runId, failedNode, error, url."
            >
              <select
                value={s.errorWorkflow ?? ""}
                onChange={(e) => setS({ ...s, errorWorkflow: e.target.value })}
                className="h-9 w-full rounded-md border border-border bg-surface px-2 text-sm text-fg"
              >
                <option value="">None</option>
                {workflows.map((w) => (
                  <option key={w.slug} value={w.slug}>
                    {w.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Email alert" help="Uses the server's SMTP settings.">
              <Input
                type="email"
                placeholder="ops@example.com"
                value={s.alertEmail ?? ""}
                onChange={(e) => setS({ ...s, alertEmail: e.target.value })}
              />
            </Field>
            <Field label="Slack alert" help="A Slack incoming-webhook URL (api.slack.com/messaging/webhooks).">
              <Input
                placeholder="https://hooks.slack.com/services/…"
                value={s.alertSlackWebhook ?? ""}
                onChange={(e) => setS({ ...s, alertSlackWebhook: e.target.value })}
              />
            </Field>
            {error && <p className="text-[12px] text-danger">{error}</p>}
          </>
        )}
      </div>
      <div className="flex justify-end gap-2 border-t border-border px-5 py-3">
        <Button variant="ghost" size="sm" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button variant="primary" size="sm" onClick={save} disabled={busy || !s}>
          {busy ? "Saving…" : "Save"}
        </Button>
      </div>
    </Dialog>
  );
}

function Field({ label, help, children }: { label: string; help: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1.5 block text-[12px] font-medium text-fg">{label}</label>
      {children}
      <p className="mt-1 text-[11px] text-faint">{help}</p>
    </div>
  );
}
