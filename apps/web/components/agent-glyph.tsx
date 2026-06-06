import {
  Compass,
  Search,
  Globe,
  Code2,
  BarChart3,
  PenLine,
  ShieldCheck,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentRole } from "@/lib/types";

const ROLE: Record<AgentRole, { icon: LucideIcon; tone: string; ring: string; label: string }> = {
  orchestrator: { icon: Compass, tone: "text-accent", ring: "ring-accent/30 bg-accent-bg", label: "Orchestrator" },
  researcher: { icon: Search, tone: "text-sky-600", ring: "ring-sky-400/25 bg-sky-500/10", label: "Researcher" },
  browser: { icon: Globe, tone: "text-emerald-600", ring: "ring-emerald-400/25 bg-emerald-500/10", label: "Browser" },
  coder: { icon: Code2, tone: "text-fuchsia-600", ring: "ring-fuchsia-400/25 bg-fuchsia-500/10", label: "Coder" },
  analyst: { icon: BarChart3, tone: "text-amber-600", ring: "ring-amber-400/25 bg-amber-500/10", label: "Analyst" },
  writer: { icon: PenLine, tone: "text-rose-600", ring: "ring-rose-400/25 bg-rose-500/10", label: "Writer" },
  validator: { icon: ShieldCheck, tone: "text-teal-600", ring: "ring-teal-400/25 bg-teal-500/10", label: "Validator" },
};

export function roleMeta(role: AgentRole) {
  return ROLE[role];
}

export function AgentGlyph({
  role,
  className,
  size = "md",
}: {
  role: AgentRole;
  className?: string;
  size?: "sm" | "md" | "lg";
}) {
  const m = ROLE[role];
  const Icon = m.icon;
  const box = size === "sm" ? "h-7 w-7" : size === "lg" ? "h-10 w-10" : "h-8 w-8";
  const ic = size === "sm" ? "size-3.5" : size === "lg" ? "size-5" : "size-4";
  return (
    <span
      className={cn(
        "inline-flex items-center justify-center rounded-lg ring-1",
        m.ring,
        box,
        className,
      )}
    >
      <Icon className={cn(ic, m.tone)} />
    </span>
  );
}
