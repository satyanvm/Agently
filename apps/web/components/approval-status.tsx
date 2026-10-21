import { Badge } from "@/components/ui/badge";
import type { ApprovalStatus } from "@/lib/marketing";

const META: Record<ApprovalStatus, { label: string; variant: "warn" | "success" | "danger" | "neutral" }> = {
  pending: { label: "Waiting", variant: "warn" },
  approved: { label: "Approved", variant: "success" },
  rejected: { label: "Rejected", variant: "danger" },
  timed_out: { label: "Timed out", variant: "neutral" },
  canceled: { label: "Canceled", variant: "neutral" },
};

export function ApprovalStatusBadge({ status }: { status: ApprovalStatus }) {
  const m = META[status] ?? { label: status, variant: "neutral" as const };
  return (
    <Badge variant={m.variant} size="sm">
      {m.label}
    </Badge>
  );
}
