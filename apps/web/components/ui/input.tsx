import * as React from "react";
import { cn } from "@/lib/utils";

export const Input = React.forwardRef<
  HTMLInputElement,
  React.InputHTMLAttributes<HTMLInputElement>
>(({ className, ...props }, ref) => (
  <input
    ref={ref}
    className={cn(
      "h-9 w-full rounded-md border border-border bg-surface px-3 text-sm text-fg placeholder:text-faint",
      "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25",
      className,
    )}
    {...props}
  />
));
Input.displayName = "Input";

export function SearchInput({
  className,
  icon,
  trailing,
  ...props
}: React.InputHTMLAttributes<HTMLInputElement> & {
  icon?: React.ReactNode;
  trailing?: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "group flex h-9 items-center gap-2 rounded-md border border-border bg-surface px-3 transition-colors focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/25",
        className,
      )}
    >
      {icon && <span className="text-faint group-focus-within:text-muted">{icon}</span>}
      <input
        className="h-full w-full bg-transparent text-sm text-fg placeholder:text-faint focus:outline-none"
        {...props}
      />
      {trailing}
    </div>
  );
}
