import {
  FileJson,
  FileText,
  Image as ImageIcon,
  Table2,
  FileCode2,
  Link2,
  File,
  Download,
} from "lucide-react";
import type { RunArtifact } from "@/lib/types";
import { cn, formatClock } from "@/lib/utils";

const KIND_META = {
  json: { icon: FileJson, tone: "text-amber-600 bg-amber-500/10 ring-amber-400/20" },
  report: { icon: FileText, tone: "text-sky-600 bg-sky-500/10 ring-sky-400/20" },
  image: { icon: ImageIcon, tone: "text-fuchsia-600 bg-fuchsia-500/10 ring-fuchsia-400/20" },
  dataset: { icon: Table2, tone: "text-emerald-600 bg-emerald-500/10 ring-emerald-400/20" },
  code: { icon: FileCode2, tone: "text-accent-soft bg-accent-bg ring-accent/20" },
  url: { icon: Link2, tone: "text-teal-600 bg-teal-500/10 ring-teal-400/20" },
  file: { icon: File, tone: "text-muted bg-surface-2 ring-border" },
} as const;

function fmtSize(bytes?: number) {
  if (!bytes) return "—";
  if (bytes >= 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`;
  if (bytes >= 1_000) return `${(bytes / 1_000).toFixed(1)} KB`;
  return `${bytes} B`;
}

export function ArtifactList({ artifacts, columns = true }: { artifacts: RunArtifact[]; columns?: boolean }) {
  return (
    <div className={cn(columns ? "grid gap-2.5 sm:grid-cols-2" : "space-y-2")}>
      {artifacts.map((a) => {
        const m = KIND_META[a.kind];
        const Icon = m.icon;
        return (
          <div
            key={a.id}
            className="group flex items-center gap-3 rounded-lg border border-border bg-surface p-3 transition-colors hover:border-border-strong hover:bg-surface-2"
          >
            <span className={cn("flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ring-1", m.tone)}>
              <Icon className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate font-mono text-[12.5px] font-medium text-fg">{a.name}</div>
              <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-faint">
                <span>{fmtSize(a.sizeBytes)}</span>
                <span className="text-ghost">·</span>
                <span className="truncate">{a.producedBy}</span>
                <span className="text-ghost">·</span>
                <span>{formatClock(a.at)}</span>
              </div>
              {a.preview && (
                <div className="mt-1.5 truncate rounded bg-inset px-2 py-1 font-mono text-[10.5px] text-faint">
                  {a.preview}
                </div>
              )}
            </div>
            <button className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-faint opacity-0 transition-opacity hover:bg-surface-3 hover:text-fg group-hover:opacity-100">
              <Download className="size-4" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
