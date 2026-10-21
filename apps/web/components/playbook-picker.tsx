"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { ArrowLeft, ArrowRight, Check, Clock, Loader2, ShieldCheck, Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiErrorMessage, fetchPlaybooks, installPlaybook, type PlaybookSummary } from "@/lib/marketing";
import { cn } from "@/lib/utils";

/**
 * "Start from a playbook": pick a ready-made template, fill in the few things
 * it needs (who approves, which doc, which channel), and it is created as a
 * playbook you can open, edit and run. Nothing is compiled by AI here.
 */
export function PlaybookPicker({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const router = useRouter();
  const [list, setList] = React.useState<PlaybookSummary[] | null>(null);
  const [chosen, setChosen] = React.useState<PlaybookSummary | null>(null);
  const [values, setValues] = React.useState<Record<string, string>>({});
  const [error, setError] = React.useState("");
  const [installing, setInstalling] = React.useState(false);

  React.useEffect(() => {
    fetchPlaybooks()
      .then(setList)
      .catch((e) => setError(apiErrorMessage(e)));
  }, []);

  function choose(pb: PlaybookSummary) {
    setChosen(pb);
    setValues(Object.fromEntries(pb.inputs.map((i) => [i.key, ""])));
    setError("");
  }

  const missing = chosen?.inputs.filter((i) => i.required && !values[i.key]?.trim()) ?? [];

  async function install() {
    if (!chosen) return;
    setInstalling(true);
    setError("");
    try {
      const { slug } = await installPlaybook(chosen.id, { inputs: values });
      onDone();
      router.push(`/workflows/${slug}`);
    } catch (e) {
      setError(apiErrorMessage(e));
      setInstalling(false);
    }
  }

  return (
    <>
      <div className="max-h-[60vh] space-y-3 overflow-y-auto px-5 py-4">
        {!chosen && (
          <>
            {list === null && !error && <p className="text-[12px] text-faint">Loading…</p>}
            {list?.map((pb) => (
              <button
                key={pb.id}
                onClick={() => choose(pb)}
                className="block w-full rounded-lg border border-border bg-surface px-4 py-3 text-left transition-colors hover:border-border-strong hover:bg-surface-2"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-[13.5px] font-semibold text-fg">{pb.name}</span>
                  <ArrowRight className="size-4 text-ghost" />
                </div>
                <p className="mt-1 text-[12.5px] leading-relaxed text-muted">{pb.summary}</p>
                <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11.5px] text-faint">
                  <span className="inline-flex items-center gap-1">
                    <Clock className="size-3" /> {pb.schedule}
                  </span>
                  <span className="inline-flex items-center gap-1">
                    <ShieldCheck className="size-3" /> {pb.approvals} approval{pb.approvals === 1 ? "" : "s"}
                  </span>
                  <span className="inline-flex items-center gap-1">
                    <Send className="size-3" /> {pb.publishesTo.join(" · ")}
                  </span>
                </div>
              </button>
            ))}
          </>
        )}

        {chosen && (
          <div className="space-y-3">
            <button onClick={() => setChosen(null)} className="inline-flex items-center gap-1 text-[12px] text-faint hover:text-fg">
              <ArrowLeft className="size-3" /> All playbooks
            </button>
            <div>
              <div className="text-[14px] font-semibold text-fg">{chosen.name}</div>
              <p className="mt-0.5 text-[12.5px] text-muted">{chosen.summary}</p>
            </div>
            {chosen.logins.length > 0 && (
              <div className="rounded-md border border-border bg-surface-2/40 px-3 py-2 text-[12px]">
                <div className="mb-1 font-medium text-muted">Sign-ins it uses</div>
                <ul className="space-y-0.5">
                  {chosen.logins.map((l) => (
                    <li key={l.type} className={cn("flex items-center gap-1.5", l.connected ? "text-success" : "text-faint")}>
                      {l.connected ? <Check className="size-3" /> : <span className="inline-block size-3 text-center">·</span>}
                      {l.label}
                      {l.connected ? " — connected" : " — not connected yet; those steps fail until you sign in (Settings → Sign-in apps, or in the builder)"}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {chosen.inputs.map((i) => (
              <label key={i.key} className="block">
                <span className="mb-1 block text-[12px] font-medium text-muted">
                  {i.label}
                  {!i.required && <span className="font-normal text-faint"> (optional)</span>}
                </span>
                <Input
                  value={values[i.key] ?? ""}
                  onChange={(e) => setValues({ ...values, [i.key]: e.target.value })}
                  placeholder={i.example ?? (i.type === "list" ? "a@example.com, b@example.com" : "")}
                />
                {i.help && <span className="mt-0.5 block text-[11.5px] text-faint">{i.help}</span>}
              </label>
            ))}
          </div>
        )}

        {error && (
          <div className="rounded-md border border-danger/30 bg-danger-bg px-3 py-2 text-[12px] text-danger">{error}</div>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        {chosen && (
          <Button variant="primary" size="sm" onClick={install} disabled={installing || missing.length > 0}>
            {installing ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
            Create playbook
          </Button>
        )}
      </div>
    </>
  );
}
