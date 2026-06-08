"use client";

import * as React from "react";
import Link from "next/link";
import { Search, Bell, Plus, ChevronRight } from "lucide-react";
import { Kbd } from "@/components/ui/kbd";
import { Avatar } from "@/components/ui/avatar";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import { CommandPalette } from "./command-palette";

export interface Crumb {
  label: string;
  href?: string;
}

export function TopBar({
  crumbs,
  actions,
}: {
  crumbs: Crumb[];
  actions?: React.ReactNode;
}) {
  const [paletteOpen, setPaletteOpen] = React.useState(false);

  React.useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <>
      <header className="sticky top-0 z-20 flex h-14 items-center gap-4 border-b border-border bg-bg/70 px-6 backdrop-blur-xl">
        <nav className="flex min-w-0 items-center gap-1.5 text-sm">
          {crumbs.map((c, i) => {
            const last = i === crumbs.length - 1;
            return (
              <React.Fragment key={i}>
                {i > 0 && <ChevronRight className="size-3.5 shrink-0 text-ghost" />}
                {c.href && !last ? (
                  <Link
                    href={c.href}
                    className="shrink-0 text-muted transition-colors hover:text-fg"
                  >
                    {c.label}
                  </Link>
                ) : (
                  <span className="truncate font-medium text-fg">{c.label}</span>
                )}
              </React.Fragment>
            );
          })}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          {actions}

          <button
            onClick={() => setPaletteOpen(true)}
            className="hidden h-9 items-center gap-2 rounded-md border border-border bg-surface px-3 text-[13px] text-faint transition-colors hover:border-border-strong hover:text-muted md:flex"
          >
            <Search className="size-3.5" />
            <span>Search…</span>
            <span className="ml-2 flex items-center gap-0.5">
              <Kbd>⌘</Kbd>
              <Kbd>K</Kbd>
            </span>
          </button>

          <ThemeToggle />

          <Link
            href="/notifications"
            className="relative flex h-9 w-9 items-center justify-center rounded-md border border-border bg-surface text-muted transition-colors hover:border-border-strong hover:text-fg"
            aria-label="Notifications"
          >
            <Bell className="size-4" />
            <span className="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-accent ring-2 ring-surface" />
          </Link>

          <button className="flex items-center gap-2 rounded-full p-0.5 pr-0.5 transition-colors hover:bg-surface-2">
            <Avatar initials="MC" />
          </button>
        </div>
      </header>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </>
  );
}

/** Convenience CTA used in several top bars. */
export function NewRunButton() {
  return (
    <button className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-medium text-accent-fg transition-colors hover:bg-accent-soft glow-accent">
      <Plus className="size-4" />
      New run
    </button>
  );
}
