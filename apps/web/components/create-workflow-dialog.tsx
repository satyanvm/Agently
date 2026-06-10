"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Sparkles, Loader2, Mail, Clock, ArrowRight } from "lucide-react";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AgentGlyph } from "@/components/agent-glyph";
import { planWorkflow, createWorkflow, type WorkflowPlan } from "@/lib/api";
import type { AgentRole } from "@/lib/types";

const EXAMPLE =
  "Every morning, pull the latest AI research papers from arXiv, hot posts from Reddit r/MachineLearning, and top AI stories from Hacker News and Google News. Summarize the highlights and email me a digest at you@gmail.com.";

/**
 * The "prompt → workflow" composer. You describe what you want in plain English; we
 * compile it (server-side, /api/workflows/plan) into an agent graph and show a live
 * preview of the agents BEFORE you create. On create, we redirect to the new
 * workflow so you can run it immediately.
 */
export function CreateWorkflowDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const router = useRouter();
  const [prompt, setPrompt] = React.useState("");
  const [plan, setPlan] = React.useState<WorkflowPlan | null>(null);
  const [planning, setPlanning] = React.useState(false);
  const [creating, setCreating] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // Debounced live preview: re-plan ~600ms after the user stops typing.
  React.useEffect(() => {
    if (!open) return;
    const text = prompt.trim();
    if (text.length < 12) {
      setPlan(null);
      return;
    }
    setPlanning(true);
    const t = setTimeout(async () => {
      try {
        const p = await planWorkflow({ prompt: text });
        setPlan(p);
        setError(null);
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to plan");
      } finally {
        setPlanning(false);
      }
    }, 600);
    return () => clearTimeout(t);
  }, [prompt, open]);

  // Reset on close.
  React.useEffect(() => {
    if (!open) {
      setPrompt("");
      setPlan(null);
      setError(null);
      setCreating(false);
    }
  }, [open]);

  const onCreate = async () => {
    if (!prompt.trim()) return;
    setCreating(true);
    setError(null);
    try {
      const { slug } = await createWorkflow({ prompt: prompt.trim() });
      onOpenChange(false);
      router.push(`/workflows/${slug}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create workflow");
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="max-w-[620px]">
      <DialogHeader
        title="New workflow from a prompt"
        subtitle="Describe the agent crew you want. We compile it into a runnable graph."
        onClose={() => onOpenChange(false)}
      />

      <div className="space-y-4 px-5 py-4">
        <div>
          <textarea
            autoFocus
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder={EXAMPLE}
            rows={4}
            className="w-full resize-none rounded-md border border-border bg-surface px-3 py-2.5 text-[13px] leading-relaxed text-fg placeholder:text-faint/70 transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25"
          />
          <button
            onClick={() => setPrompt(EXAMPLE)}
            className="mt-1.5 inline-flex items-center gap-1.5 text-[11px] text-muted transition-colors hover:text-fg"
          >
            <Sparkles className="size-3" /> Use the example
          </button>
        </div>

        {/* Live preview of the compiled graph */}
        <div className="rounded-lg border border-border bg-surface-2/40 p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-[11px] font-semibold uppercase tracking-wider text-ghost">
              Preview
            </span>
            {planning && <Loader2 className="size-3.5 animate-spin text-faint" />}
          </div>

          {!plan && !planning && (
            <p className="py-3 text-center text-[12px] text-faint">
              Start typing — the agents you’ll get appear here.
            </p>
          )}

          {plan && (
            <div className="space-y-3">
              <div className="text-[13px] font-medium text-fg">{plan.name}</div>
              <div className="flex flex-wrap gap-1.5">
                {plan.nodes.map((n) => (
                  <span
                    key={n.key}
                    className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-2 py-1 text-[11px] text-muted"
                  >
                    <AgentGlyph role={n.role as AgentRole} size="sm" />
                    {n.name}
                  </span>
                ))}
              </div>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-faint">
                {typeof plan.defaultInput.email === "string" && plan.defaultInput.email && (
                  <span className="inline-flex items-center gap-1">
                    <Mail className="size-3" /> {plan.defaultInput.email as string}
                  </span>
                )}
                {plan.schedule && (
                  <span className="inline-flex items-center gap-1">
                    <Clock className="size-3" /> {plan.schedule}
                  </span>
                )}
                <span>{plan.nodes.length} agents</span>
              </div>
            </div>
          )}
        </div>

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
        <Button variant="primary" size="sm" onClick={onCreate} disabled={!prompt.trim() || creating}>
          {creating ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
          Create workflow
        </Button>
      </div>
    </Dialog>
  );
}
