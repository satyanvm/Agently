"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import * as React from "react";
import {
  LayoutDashboard,
  Inbox,
  Radar,
  Megaphone,
  Workflow,
  ListTree,
  Bot,
  Globe,
  Bell,
  Settings,
  ChevronsUpDown,
  Plus,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { fetchApprovals } from "@/lib/marketing";

const NAV = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/inbox", label: "Inbox", icon: Inbox },
  // "Playbooks" is the UI name for workflows; routes and the API keep /workflows.
  { href: "/workflows", label: "Playbooks", icon: Workflow },
  { href: "/competitors", label: "Competitors", icon: Radar },
  { href: "/brand", label: "Brand", icon: Megaphone },
  { href: "/runs", label: "Runs", icon: ListTree },
  { href: "/agents", label: "Agents", icon: Bot },
  { href: "/browser", label: "Browser Sessions", icon: Globe },
  { href: "/notifications", label: "Notifications", icon: Bell },
] as const;

export function Sidebar() {
  const pathname = usePathname();
  const pending = usePendingApprovals();

  return (
    <aside className="fixed inset-y-0 left-0 z-30 flex w-[244px] flex-col border-r border-border bg-bg">
      {/* Brand + workspace switcher */}
      <div className="px-3 pt-3.5">
        <button className="group flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left transition-colors hover:bg-surface-2">
          <span className="relative flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-accent to-accent-soft">
            <Logo className="h-4 w-4 text-white" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[13px] font-semibold leading-tight text-fg">
              Agently
            </span>
            <span className="block truncate text-[11px] leading-tight text-faint">
              Signals → approved actions
            </span>
          </span>
          <ChevronsUpDown className="size-3.5 text-faint group-hover:text-muted" />
        </button>
      </div>

      {/* New playbook */}
      <div className="px-3 py-3">
        <Link
          href="/workflows?new=1"
          className="flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-accent text-[13px] font-medium text-accent-fg transition-colors hover:bg-accent-soft"
        >
          <Plus className="size-4" />
          New playbook
        </Link>
      </div>

      {/* Nav */}
      <nav className="flex-1 space-y-0.5 px-3">
        {NAV.map((item) => {
          const active =
            pathname === item.href || pathname.startsWith(item.href + "/");
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "group relative flex items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-[13px] font-medium transition-colors",
                active
                  ? "bg-surface-2 text-fg"
                  : "text-muted hover:bg-surface-2/60 hover:text-fg",
              )}
            >
              {active && (
                <span className="absolute left-0 top-1/2 h-4 w-[3px] -translate-x-3 -translate-y-1/2 rounded-r-full bg-accent" />
              )}
              <Icon
                className={cn(
                  "size-[17px] shrink-0",
                  active ? "text-accent-soft" : "text-faint group-hover:text-muted",
                )}
              />
              <span className="flex-1">{item.label}</span>
              {item.href === "/inbox" && pending > 0 && (
                <span className="rounded-full bg-warn-bg px-1.5 text-[11px] font-semibold text-warn ring-1 ring-warn/25">
                  {pending}
                </span>
              )}
            </Link>
          );
        })}
      </nav>

      <div className="mt-auto px-3 pb-3">
        <Link
          href="/settings"
          className={cn(
            "group flex items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-[13px] font-medium transition-colors",
            pathname.startsWith("/settings")
              ? "bg-surface-2 text-fg"
              : "text-muted hover:bg-surface-2/60 hover:text-fg",
          )}
        >
          <Settings className="size-[17px] text-faint group-hover:text-muted" />
          Settings
        </Link>
      </div>
    </aside>
  );
}

/** Drafts waiting for a decision, refreshed every 15s (0 while unknown). */
function usePendingApprovals(): number {
  const [n, setN] = React.useState(0);
  React.useEffect(() => {
    let active = true;
    const load = () =>
      fetchApprovals("pending")
        .then((rows) => active && setN(rows.length))
        .catch(() => {});
    load();
    const t = setInterval(load, 15000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, []);
  return n;
}

function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path
        d="M12 3l2.4 5.1L20 9.3l-4 4 1 5.7-5-2.8-5 2.8 1-5.7-4-4 5.6-1.2L12 3z"
        fill="currentColor"
        opacity="0.92"
      />
    </svg>
  );
}
