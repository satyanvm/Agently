"use client";

import { cn } from "@/lib/utils";

export function Switch({
  checked,
  onChange,
  className,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  className?: string;
}) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative inline-flex h-[20px] w-[34px] shrink-0 items-center rounded-full transition-colors",
        checked ? "bg-accent" : "bg-surface-3",
        className,
      )}
    >
      <span
        className={cn(
          "inline-block h-[15px] w-[15px] transform rounded-full bg-white shadow transition-transform",
          checked ? "translate-x-[16px]" : "translate-x-[3px]",
        )}
      />
    </button>
  );
}
