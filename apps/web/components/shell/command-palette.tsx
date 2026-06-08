"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import {
  LayoutDashboard,
  Workflow,
  ListTree,
  Bot,
  Globe,
  Bell,
  Settings,
  Search,
  CornerDownLeft,
  Play,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Kbd } from "@/components/ui/kbd";
import { fetchWorkflows } from "@/lib/api";
import type { Workflow as WorkflowModel } from "@/lib/types";

interface Item {
  id: string;
  label: string;
  hint: string;
  icon: React.ComponentType<{ className?: string }>;
  href: string;
  group: string;
}

const NAV_ITEMS: Item[] = [
  { id: "dash", label: "Dashboard", hint: "Overview", icon: LayoutDashboard, href: "/dashboard", group: "Navigate" },
  { id: "wf", label: "Workflows", hint: "All workflows", icon: Workflow, href: "/workflows", group: "Navigate" },
  { id: "runs", label: "Runs", hint: "Execution history", icon: ListTree, href: "/runs", group: "Navigate" },
  { id: "agents", label: "Agents", hint: "Agent library", icon: Bot, href: "/agents", group: "Navigate" },
  { id: "browser", label: "Browser Sessions", hint: "Live browsers", icon: Globe, href: "/browser", group: "Navigate" },
  { id: "notif", label: "Notifications", hint: "Alerts & events", icon: Bell, href: "/notifications", group: "Navigate" },
  { id: "settings", label: "Settings", hint: "Workspace", icon: Settings, href: "/settings", group: "Navigate" },
];

export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const router = useRouter();
  const [q, setQ] = React.useState("");
  const [active, setActive] = React.useState(0);
  const [workflows, setWorkflows] = React.useState<WorkflowModel[]>([]);

  React.useEffect(() => {
    if (!open) return;
    let live = true;
    fetchWorkflows().then((w) => live && setWorkflows(w)).catch(() => {});
    return () => {
      live = false;
    };
  }, [open]);

  const items: Item[] = React.useMemo(() => {
    const wf: Item[] = workflows.map((w) => ({
      id: w.id,
      label: w.name,
      hint: `${w.agentCount} agents`,
      icon: Play,
      href: `/workflows/${w.slug}`,
      group: "Workflows",
    }));
    const all = [...NAV_ITEMS, ...wf];
    if (!q) return all;
    const needle = q.toLowerCase();
    return all.filter((i) => i.label.toLowerCase().includes(needle));
  }, [q, workflows]);

  React.useEffect(() => setActive(0), [q]);

  React.useEffect(() => {
    if (!open) setQ("");
  }, [open]);

  const go = React.useCallback(
    (item?: Item) => {
      const target = item ?? items[active];
      if (target) {
        router.push(target.href);
        onOpenChange(false);
      }
    },
    [items, active, router, onOpenChange],
  );

  function onKey(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, items.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      go();
    } else if (e.key === "Escape") {
      onOpenChange(false);
    }
  }

  if (!open) return null;

  let lastGroup = "";

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center px-4 pt-[14vh]"
      onClick={() => onOpenChange(false)}
    >
      <div className="absolute inset-0 bg-[#0b0e1a]/30 backdrop-blur-[2px] animate-in" />
      <div
        className="relative w-full max-w-[560px] overflow-hidden rounded-xl border border-border-strong bg-surface shadow-pop animate-in"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-border px-4">
          <Search className="size-4 text-faint" />
          <input
            autoFocus
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={onKey}
            placeholder="Search workflows, runs, agents…"
            className="h-12 w-full bg-transparent text-sm text-fg placeholder:text-faint focus:outline-none"
          />
          <Kbd>Esc</Kbd>
        </div>

        <div className="max-h-[340px] overflow-y-auto p-2">
          {items.length === 0 && (
            <div className="px-3 py-8 text-center text-sm text-faint">
              No results for “{q}”
            </div>
          )}
          {items.map((item, i) => {
            const showGroup = item.group !== lastGroup;
            lastGroup = item.group;
            const Icon = item.icon;
            return (
              <React.Fragment key={item.id}>
                {showGroup && (
                  <div className="px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-ghost">
                    {item.group}
                  </div>
                )}
                <button
                  onMouseEnter={() => setActive(i)}
                  onClick={() => go(item)}
                  className={cn(
                    "flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors",
                    i === active ? "bg-surface-2" : "hover:bg-surface-2/50",
                  )}
                >
                  <Icon
                    className={cn(
                      "size-4 shrink-0",
                      i === active ? "text-accent-soft" : "text-faint",
                    )}
                  />
                  <span className="flex-1 text-[13px] text-fg">{item.label}</span>
                  <span className="text-[11px] text-faint">{item.hint}</span>
                  {i === active && <CornerDownLeft className="size-3.5 text-faint" />}
                </button>
              </React.Fragment>
            );
          })}
        </div>
      </div>
    </div>
  );
}
