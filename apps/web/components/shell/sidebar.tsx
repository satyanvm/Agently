"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
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

const NAV = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/workflows", label: "Workflows", icon: Workflow },
  { href: "/runs", label: "Runs", icon: ListTree },
  { href: "/agents", label: "Agents", icon: Bot },
  { href: "/browser", label: "Browser Sessions", icon: Globe },
  { href: "/notifications", label: "Notifications", icon: Bell },
] as const;

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="fixed inset-y-0 left-0 z-30 flex w-[244px] flex-col border-r border-border bg-bg">
      {/* Brand + workspace switcher */}
      <div className="px-3 pt-3.5">
        <button className="group flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left transition-colors hover:bg-surface-2">
          <span className="relative flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-accent to-accent-soft shadow-[0_4px_14px_-2px_rgba(94,79,242,0.5)]">
            <Logo className="h-4 w-4 text-white" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[13px] font-semibold leading-tight text-fg">
              Agently
            </span>
            <span className="block truncate text-[11px] leading-tight text-faint">
              Durable workflows
            </span>
          </span>
          <ChevronsUpDown className="size-3.5 text-faint group-hover:text-muted" />
        </button>
      </div>

      {/* New workflow */}
      <div className="px-3 py-3">
        <Link
          href="/workflows?new=1"
          className="flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-accent text-[13px] font-medium text-accent-fg transition-colors hover:bg-accent-soft glow-accent"
        >
          <Plus className="size-4" />
          New workflow
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
