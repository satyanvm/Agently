"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Play, Loader2 } from "lucide-react";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { launchRun } from "@/lib/api";
import type { Workflow } from "@/lib/types";

/**
 * Launch dialog: pre-fills topic + recipient email from the workflow's stored default
 * input (the plan), lets you tweak them for this one run, then launches and navigates
 * to the live run page. Per-run values override the workflow defaults server-side.
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
  const [topic, setTopic] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [launching, setLaunching] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (open) {
      setTopic(typeof defaults.topic === "string" ? defaults.topic : "");
      setEmail(typeof defaults.email === "string" ? defaults.email : "");
      setError(null);
      setLaunching(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const onLaunch = async () => {
    setLaunching(true);
    setError(null);
    try {
      const input: Record<string, unknown> = {};
      if (topic.trim()) input.topic = topic.trim();
      if (email.trim()) input.email = email.trim();
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
        <Field label="Topic">
          <Input value={topic} onChange={(e) => setTopic(e.target.value)} placeholder="AI" />
        </Field>
        <Field label="Email the digest to">
          <Input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="you@gmail.com"
          />
        </Field>
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

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] font-medium text-muted">{label}</span>
      {children}
    </label>
  );
}
