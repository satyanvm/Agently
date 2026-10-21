"use client";

import * as React from "react";
import { ChevronRight, Pencil, Pin, PinOff } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { outputRef, type NodeSample, type NodeSamples } from "@/lib/builder-debug";
import { JsonTree } from "./json-tree";

/**
 * The inspector's Data tab (n8n's input/output panels).
 *
 * INPUT: the latest output of every direct dependency — the only data this
 * node's templates can reference. Keys are draggable onto parameter fields.
 * OUTPUT: this node's pinned data if any, else its latest run sample, with
 * pin / unpin / edit. Pinned data is what test runs return instead of
 * executing the node (production runs ignore pins).
 */
export function NodeDataPanel({
  nodeKey,
  deps,
  samples,
  pinned,
  onPin,
  onUnpin,
}: {
  nodeKey: string;
  /** Direct dependencies; `pinned` is their pinned data, which test runs feed in. */
  deps: { key: string; label: string; pinned?: Record<string, unknown> }[];
  samples: NodeSamples | null;
  pinned: Record<string, unknown> | undefined;
  onPin: (output: Record<string, unknown>) => void;
  onUnpin: () => void;
}) {
  const own = samples?.[nodeKey];
  return (
    <div className="space-y-5">
      <section>
        <SectionTitle>Input</SectionTitle>
        {deps.length === 0 ? (
          <Empty>This node has no upstream nodes. It can still read the run input via {"{{input.<field>}}"}.</Empty>
        ) : (
          <>
            <p className="mb-2 text-[11px] text-faint">Drag a field onto a parameter to map it.</p>
            <div className="space-y-2">
              {deps.map((d) => (
                <DepOutput key={d.key} dep={d} sample={samples?.[d.key]} />
              ))}
            </div>
          </>
        )}
      </section>

      <section>
        <div className="mb-1.5 flex items-center justify-between">
          <SectionTitle>Output</SectionTitle>
          {pinned && (
            <Badge size="sm" variant="accent">
              <Pin className="size-2.5" /> Pinned
            </Badge>
          )}
        </div>
        <OwnOutput sample={own} pinned={pinned} onPin={onPin} onUnpin={onUnpin} />
      </section>
    </div>
  );
}

export function DepOutput({
  dep,
  sample,
}: {
  dep: { key: string; label: string; pinned?: Record<string, unknown> };
  sample?: NodeSample;
}) {
  // Pinned data wins: it is what a test run hands this node.
  const data = dep.pinned ?? sample?.output;
  return (
    <div className="rounded-md border border-border bg-surface-2 px-2.5 py-2">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="truncate text-[12px] font-medium text-fg">{dep.label}</span>
        {dep.pinned ? (
          <span className="inline-flex shrink-0 items-center gap-0.5 text-[10px] text-accent">
            <Pin className="size-2.5" /> pinned
          </span>
        ) : (
          sample && <span className="shrink-0 text-[10px] text-faint">run #{sample.runNumber}</span>
        )}
      </div>
      {data ? (
        <JsonTree value={data} refFor={(path) => outputRef(dep.key, path)} />
      ) : sample?.error ? (
        <p className="text-[11px] text-danger">Failed: {sample.error}</p>
      ) : (
        <Empty>No data yet — run this node (or the whole workflow) once.</Empty>
      )}
    </div>
  );
}

function OwnOutput({
  sample,
  pinned,
  onPin,
  onUnpin,
}: {
  sample?: NodeSample;
  pinned?: Record<string, unknown>;
  onPin: (o: Record<string, unknown>) => void;
  onUnpin: () => void;
}) {
  const [editing, setEditing] = React.useState(false);
  const [draft, setDraft] = React.useState("");
  const [err, setErr] = React.useState<string | null>(null);

  const startEdit = () => {
    setDraft(JSON.stringify(pinned ?? sample?.output ?? {}, null, 2));
    setErr(null);
    setEditing(true);
  };
  const saveEdit = () => {
    try {
      const v = JSON.parse(draft);
      if (!v || typeof v !== "object" || Array.isArray(v)) throw new Error("Pinned data must be a JSON object");
      onPin(v as Record<string, unknown>);
      setEditing(false);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Invalid JSON");
    }
  };

  if (editing) {
    return (
      <div className="space-y-2">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          spellCheck={false}
          className="h-48 w-full resize-y rounded-md border border-border bg-surface-2 p-2 font-mono text-[11px] text-fg outline-none focus:border-accent"
        />
        {err && <p className="text-[11px] text-danger">{err}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>
            Cancel
          </Button>
          <Button variant="primary" size="sm" onClick={saveEdit}>
            <Pin className="size-3.5" /> Pin this data
          </Button>
        </div>
      </div>
    );
  }

  const shown = pinned ?? sample?.output;
  return (
    <div className="space-y-2">
      {!pinned && sample && (
        <div className="flex items-center gap-2 text-[11px] text-faint">
          <Badge size="sm" variant={sample.status === "succeeded" ? "success" : "danger"}>
            {sample.status}
          </Badge>
          run #{sample.runNumber}
          {sample.attempts && sample.attempts > 1 ? ` · ${sample.attempts} attempts` : ""}
        </div>
      )}
      {!pinned && sample?.error && (
        <p className="rounded-md bg-danger-bg px-2 py-1.5 text-[11px] text-danger">{sample.error}</p>
      )}
      {shown ? (
        <div className="max-h-[320px] overflow-auto rounded-md border border-border bg-surface-2 px-2.5 py-2">
          <JsonTree value={shown} />
        </div>
      ) : (
        <Empty>No output yet. Use “Run step” to execute just this node.</Empty>
      )}
      <div className="flex flex-wrap gap-2">
        {pinned ? (
          <Button variant="secondary" size="sm" onClick={onUnpin}>
            <PinOff className="size-3.5" /> Unpin
          </Button>
        ) : (
          sample?.output && (
            <Button variant="secondary" size="sm" onClick={() => onPin(sample.output!)}>
              <Pin className="size-3.5" /> Pin output
            </Button>
          )
        )}
        <Button variant="ghost" size="sm" onClick={startEdit}>
          <Pencil className="size-3.5" /> {pinned ? "Edit pinned data" : "Set mock data"}
        </Button>
      </div>
      <p className="text-[10px] leading-relaxed text-faint">
        Pinned data is returned instead of running this node in test runs, so you can iterate on
        downstream nodes without calling the service again. Scheduled and production runs ignore it.
      </p>
    </div>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <h4 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">{children}</h4>;
}

function Empty({ children }: { children: React.ReactNode }) {
  return <p className="text-[11px] leading-relaxed text-faint">{children}</p>;
}

/**
 * Upstream outputs above the parameter fields, so a field can be dragged
 * straight onto a parameter (n8n shows input beside parameters for this).
 */
export function UpstreamFields({
  deps,
  samples,
}: {
  deps: { key: string; label: string; pinned?: Record<string, unknown> }[];
  samples: NodeSamples | null;
}) {
  const hasData = deps.some((d) => d.pinned || samples?.[d.key]?.output);
  const [open, setOpen] = React.useState(hasData);
  if (deps.length === 0) return null;
  return (
    <div className="rounded-md border border-dashed border-border">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-1 px-2.5 py-2 text-left text-[11px] font-semibold uppercase tracking-wide text-muted hover:text-fg"
      >
        <ChevronRight className={`size-3 transition-transform ${open ? "rotate-90" : ""}`} />
        Map from upstream
        <span className="ml-auto font-normal normal-case tracking-normal text-faint">drag onto a field</span>
      </button>
      {open && (
        <div className="max-h-[260px] space-y-2 overflow-auto px-2.5 pb-2.5">
          {deps.map((d) => (
            <DepOutput key={d.key} dep={d} sample={samples?.[d.key]} />
          ))}
        </div>
      )}
    </div>
  );
}
