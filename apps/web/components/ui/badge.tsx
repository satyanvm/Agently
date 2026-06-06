import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badge = cva(
  "inline-flex items-center gap-1 rounded-full font-medium whitespace-nowrap",
  {
    variants: {
      variant: {
        neutral: "bg-surface-2 text-muted ring-1 ring-border",
        accent: "bg-accent-bg text-accent-soft ring-1 ring-accent/25",
        success: "bg-success-bg text-success ring-1 ring-success/20",
        warn: "bg-warn-bg text-warn ring-1 ring-warn/20",
        danger: "bg-danger-bg text-danger ring-1 ring-danger/20",
        outline: "ring-1 ring-border text-faint",
      },
      size: {
        sm: "px-2 py-0.5 text-[10px]",
        md: "px-2.5 py-0.5 text-[11px]",
      },
    },
    defaultVariants: { variant: "neutral", size: "md" },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badge> {}

export function Badge({ className, variant, size, ...props }: BadgeProps) {
  return <span className={cn(badge({ variant, size }), className)} {...props} />;
}
