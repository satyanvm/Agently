"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Play, Loader2 } from "lucide-react";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { launchRun } from "@/lib/api";
import { fetchPlaybooks, type PlaybookInput } from "@/lib/marketing";
import type { Workflow } from "@/lib/types";

type Field = { key: string; label: string; help?: string; list: boolean; initial: string };

const humanize = (key: string) => {
  const words = key.replace(/([a-z])([A-Z])/g, "$1 $2").replace(/[_-]+/g, " ").trim().toLowerCase();
  return words.charAt(0).toUpperCase() + words.slice(1);
};

/** The run's editable inputs: the workflow's stored default input (text and
 *  lists only), labelled from the playbook template when it came from one. */
function fieldsFor(defaults: Record<string, unknown>, labels: Map<string, PlaybookInput>): Field[] {
  const out: Field[] = [];
  for (const [key, value] of Object.entries(defaults)) {
    const list = Array.isArray(value);
    if (!list && typeof value !== "string") continue;
    if (list && !(value as unknown[]).every((v) => typeof v === "string")) continue;
    const def = labels.get(key);
    out.push({
      key,
      label: def?.label ?? humanize(key),
      help: def?.help,
      list,
      initial: list ? (value as string[]).join(", ") : (value as string),
    });
  }
  // The template's order when there is one; keys it doesn't know go last.
  const order = [...labels.keys()];
  const rank = (k: string) => (order.indexOf(k) + order.length + 1) % (order.length + 1);
  return out.sort((a, b) => rank(a.key) - rank(b.key));
}

/**
 * Launch dialog: pre-fills the workflow's stored default input (the plan, or a
 * playbook's install values), lets you tweak them for this one run, then
 * launches and navigates to the live run page. Only changed values are sent;
 * per-run values override the workflow defaults server-side.
 */
export function RunWorkflowDialog({
  open,
  onOpenChange,
  workflow,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workflow: Workflow;
}) {
  const router = useRouter();
  const defaults = (workflow.defaultInput ?? {}) as Record<string, unknown>;
  const playbookId = workflow.tags?.[0] === "playbook" ? workflow.tags[1] : undefined;
  const [labels, setLabels] = React.useState<Map<string, PlaybookInput>>(new Map());
  const [values, setValues] = React.useState<Record<string, string>>({});
  const [launching, setLaunching] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const fields = fieldsFor(defaults, labels);

  React.useEffect(() => {
    if (!open) return;
    setValues(Object.fromEntries(fieldsFor(defaults, new Map()).map((f) => [f.key, f.initial])));
    setError(null);
    setLaunching(false);
    if (playbookId) {
      fetchPlaybooks()
        .then((all) => {
          const pb = all.find((p) => p.id === playbookId);
          if (pb) setLabels(new Map(pb.inputs.map((i) => [i.key, i])));
        })
        .catch(() => {}); // labels fall back to the input keys
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const onLaunch = async () => {
    setLaunching(true);
    setError(null);
    try {
      const input: Record<string, unknown> = {};
      for (const f of fields) {
        const v = (values[f.key] ?? "").trim();
        if (v === f.initial.trim()) continue;
        input[f.key] = f.list ? v.split(/[,\n;]/).map((x) => x.trim()).filter(Boolean) : v;
      }
      const runId = await launchRun(workflow.slug, input);
      onOpenChange(false);
      router.push(`/runs/${runId}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to launch run");
      setLaunching(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogHeader
        title={`Run ${workflow.name}`}
        subtitle="Tweak the inputs for this run, then launch. You’ll watch it live."
        onClose={() => onOpenChange(false)}
      />
      <div className="space-y-3 px-5 py-4">
        {fields.length === 0 && (
          <p className="text-[12px] text-muted">This playbook takes no inputs.</p>
        )}
        {fields.map((f) => (
          <FieldRow key={f.key} label={f.label} help={f.help}>
            <Input
              value={values[f.key] ?? ""}
              onChange={(e) => setValues((prev) => ({ ...prev, [f.key]: e.target.value }))}
            />
          </FieldRow>
        ))}
        {error && (
          <div className="rounded-md border border-danger/30 bg-danger-bg px-3 py-2 text-[12px] text-danger">
            {error}
          </div>
        )}
      </div>
      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
        <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button variant="primary" size="sm" onClick={onLaunch} disabled={launching}>
          {launching ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
          Launch run
        </Button>
      </div>
    </Dialog>
  );
}

function FieldRow({ label, help, children }: { label: string; help?: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] font-medium text-muted">{label}</span>
      {children}
      {help && <span className="mt-1 block text-[11px] text-faint">{help}</span>}
    </label>
  );
}
