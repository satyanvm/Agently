import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const button = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md font-medium transition-all duration-150 disabled:opacity-50 disabled:pointer-events-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/60 [&_svg]:shrink-0 select-none active:scale-[0.985]",
  {
    variants: {
      variant: {
        primary:
          "bg-accent text-accent-fg hover:bg-accent-soft glow-accent",
        secondary:
          "bg-surface-2 text-fg ring-hairline hover:bg-surface-3 hover:ring-border-strong",
        ghost: "text-muted hover:text-fg hover:bg-surface-2",
        outline:
          "ring-hairline text-fg hover:bg-surface-2 hover:ring-border-strong",
        danger: "bg-danger-bg text-danger ring-1 ring-danger/30 hover:bg-danger/20",
        glass: "glass ring-hairline text-fg hover:ring-border-strong",
      },
      size: {
        sm: "h-8 px-3 text-[13px] [&_svg]:size-3.5",
        md: "h-9 px-4 text-sm [&_svg]:size-4",
        lg: "h-11 px-6 text-[15px] [&_svg]:size-[18px]",
        icon: "h-9 w-9 [&_svg]:size-4",
        "icon-sm": "h-8 w-8 [&_svg]:size-4",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof button> {}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, ...props }, ref) => (
    <button ref={ref} className={cn(button({ variant, size }), className)} {...props} />
  ),
);
Button.displayName = "Button";

export { button as buttonVariants };
