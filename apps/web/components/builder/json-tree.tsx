"use client";

import * as React from "react";
import { ChevronRight, GripVertical } from "lucide-react";
import { REF_MIME } from "@/lib/builder-debug";
import { cn } from "@/lib/utils";

/**
 * Collapsible JSON viewer. When `refFor` is given every key is draggable and
 * carries the template reference for its path — drop it into any parameter
 * field to map that value (n8n-style drag-to-map).
 */
export function JsonTree({
  value,
  refFor,
  defaultOpenDepth = 1,
}: {
  value: unknown;
  refFor?: (path: (string | number)[]) => string;
  defaultOpenDepth?: number;
}) {
  return (
    <div className="font-mono text-[11px] leading-[18px]">
      <Entry name={null} value={value} path={[]} depth={0} refFor={refFor} openDepth={defaultOpenDepth} />
    </div>
  );
}

function Entry({
  name,
  value,
  path,
  depth,
  refFor,
  openDepth,
}: {
  name: string | number | null;
  value: unknown;
  path: (string | number)[];
  depth: number;
  refFor?: (path: (string | number)[]) => string;
  openDepth: number;
}) {
  const isObj = value !== null && typeof value === "object";
  const [open, setOpen] = React.useState(depth < openDepth);
  const entries = isObj
    ? Array.isArray(value)
      ? value.map((v, i) => [i, v] as const)
      : Object.entries(value as Record<string, unknown>)
    : [];
  const draggable = Boolean(refFor) && name !== null;

  const onDragStart = (e: React.DragEvent) => {
    if (!refFor) return;
    const ref = refFor(path);
    e.dataTransfer.setData(REF_MIME, ref);
    e.dataTransfer.setData("text/plain", ref);
    e.dataTransfer.effectAllowed = "copy";
  };

  const label =
    name === null ? null : (
      <span
        draggable={draggable}
        onDragStart={onDragStart}
        title={draggable ? `Drag into a field to insert ${refFor!(path)}` : undefined}
        className={cn(
          "inline-flex items-center gap-0.5 rounded px-0.5",
          typeof name === "number" ? "text-faint" : "text-accent-soft",
          draggable && "cursor-grab hover:bg-accent-bg active:cursor-grabbing",
        )}
      >
        {draggable && <GripVertical className="size-2.5 opacity-40" />}
        {name}
      </span>
    );

  if (!isObj) {
    return (
      <div className="flex min-w-0 gap-1.5" style={{ paddingLeft: depth * 12 }}>
        {label}
        {label && <span className="text-faint">:</span>}
        <Scalar value={value} />
      </div>
    );
  }

  const summary = Array.isArray(value) ? `[${entries.length}]` : `{${entries.length}}`;
  return (
    <div>
      <div className="flex min-w-0 items-center gap-1" style={{ paddingLeft: depth * 12 }}>
        <button onClick={() => setOpen((o) => !o)} className="text-faint hover:text-fg" aria-label="Toggle">
          <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
        </button>
        {label}
        <span className="text-faint">{summary}</span>
      </div>
      {open &&
        entries.slice(0, 200).map(([k, v]) => (
          <Entry
            key={String(k)}
            name={k}
            value={v}
            path={[...path, k]}
            depth={depth + 1}
            refFor={refFor}
            openDepth={openDepth}
          />
        ))}
      {open && entries.length > 200 && (
        <div className="text-faint" style={{ paddingLeft: (depth + 1) * 12 }}>
          … {entries.length - 200} more
        </div>
      )}
    </div>
  );
}

function Scalar({ value }: { value: unknown }) {
  if (value === null || value === undefined) return <span className="text-faint">null</span>;
  if (typeof value === "string") {
    const s = value.length > 300 ? value.slice(0, 300) + "…" : value;
    return <span className="min-w-0 break-all text-success">&quot;{s}&quot;</span>;
  }
  if (typeof value === "number") return <span className="text-running">{value}</span>;
  return <span className="text-warn">{String(value)}</span>;
}
