"use client";

import * as React from "react";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { NodeSettings } from "@/lib/builder-debug";

/**
 * Per-node failure handling (n8n's node Settings tab), stored under the
 * reserved `__settings` config key and applied by the reasoner workflow
 * (workflow.py _execute): retries with a wait between attempts, a hard
 * timeout per attempt, and continue-on-fail.
 */
export function NodeSettingsPanel({
  settings,
  onChange,
}: {
  settings: NodeSettings;
  onChange: (next: NodeSettings) => void;
}) {
  const set = (patch: Partial<NodeSettings>) => onChange({ ...settings, ...patch });
  const num = (v: string): number | undefined => (v.trim() === "" ? undefined : Math.max(0, Number(v)));

  return (
    <div className="space-y-5">
      <Row
        label="Retry on fail"
        help="Run the node again when it fails, up to this many extra times."
      >
        <Input
          type="number"
          min={0}
          max={10}
          value={settings.retries ?? ""}
          placeholder="0"
          onChange={(e) => set({ retries: num(e.target.value) })}
          className="w-20"
        />
      </Row>

      {(settings.retries ?? 0) > 0 && (
        <Row label="Wait between tries (s)" help="Durable wait — survives a worker restart.">
          <Input
            type="number"
            min={0}
            value={settings.retryWaitSec ?? ""}
            placeholder="5"
            onChange={(e) => set({ retryWaitSec: num(e.target.value) })}
            className="w-20"
          />
        </Row>
      )}

      <Row label="Timeout (s)" help="Fail an attempt that runs longer than this. Empty = default (10 min).">
        <Input
          type="number"
          min={0}
          value={settings.timeoutSec ?? ""}
          placeholder="600"
          onChange={(e) => set({ timeoutSec: num(e.target.value) })}
          className="w-20"
        />
      </Row>

      <Row
        label="Continue on fail"
        help={'If every attempt fails, output {"error": "…"} and keep running the workflow instead of stopping it.'}
      >
        <Switch checked={Boolean(settings.continueOnFail)} onChange={(v) => set({ continueOnFail: v })} />
      </Row>
    </div>
  );
}

function Row({ label, help, children }: { label: string; help: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <div className="text-[12px] font-medium text-fg">{label}</div>
        <p className="mt-0.5 text-[11px] leading-relaxed text-faint">{help}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}
