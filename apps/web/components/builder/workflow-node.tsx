"use client";

import * as React from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { CheckCircle2, KeyRound, Pin, TriangleAlert, XCircle } from "lucide-react";
import { CREDENTIAL_ID_KEY, findNodeSpec } from "./node-catalog";
import { useCredentials } from "./credentials-context";
import { useRunData } from "./run-data-context";
import { PINNED_KEY } from "@/lib/builder-debug";
import { cn } from "@/lib/utils";
import type { WorkflowNodeType } from "./workflow-builder";

export function WorkflowNode({ id, data, selected }: NodeProps<WorkflowNodeType>) {
  const spec = findNodeSpec(data.typeId);
  const { credentials } = useCredentials();
  const sample = useRunData()?.[id];
  if (!spec) return null;

  const Icon = spec.icon;

  // Credential state (contract §6): a node with a credentialType needs a
  // __credentialId that points at an existing credential. While the list hasn't
  // loaded (or the API is unreachable) a set id is trusted — no badge flicker.
  const credId = data.config[CREDENTIAL_ID_KEY];
  const hasCredId = typeof credId === "string" && credId !== "";
  const dangling =
    hasCredId && credentials !== null && !credentials.some((c) => c.id === credId);
  const needsCredentials = Boolean(spec.credentialType) && (!hasCredId || dangling);
  const credentialsConfigured = Boolean(spec.credentialType) && hasCredId && !dangling;

  // "n/m configured" over real config fields only — never the reserved
  // builder keys (__credentialId, __pinned, __settings, _position).
  const configEntries = Object.entries(data.config).filter(([key]) => !key.startsWith("_"));
  const pinned = Boolean(data.config[PINNED_KEY]);
  const configuredCount = configEntries.filter(([, v]) => v !== "").length;

  return (
    <div
      className={cn(
        "relative min-w-[200px] rounded-lg border-2 bg-surface shadow-md transition-all",
        selected
          ? "border-accent shadow-lg ring-2 ring-accent/20"
          : "border-border hover:border-border-strong hover:shadow-lg"
      )}
    >
      {/* Input handle */}
      {spec.kind !== "trigger" && (
        <Handle
          type="target"
          position={Position.Top}
          className="!h-3 !w-3 !border-2 !border-border-strong !bg-surface"
        />
      )}

      {/* Node content */}
      <div className="p-3">
        <div className="flex items-start gap-2.5">
          <div className={cn("flex h-8 w-8 shrink-0 items-center justify-center rounded-md", spec.toneBg)}>
            <Icon className={cn("size-4", spec.tone)} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-semibold text-fg leading-tight">{data.label}</div>
            <div className="flex items-center gap-1 text-[11px] text-muted mt-0.5">
              {spec.kind}
              {credentialsConfigured && (
                <KeyRound className="size-3 text-faint" aria-label="Credentials configured" />
              )}
              {pinned && <Pin className="size-3 text-accent" aria-label="Output pinned" />}
            </div>
          </div>
          {/* Last run result for this node (from the Data tab's samples). */}
          {sample?.status === "succeeded" && (
            <CheckCircle2 className="size-4 shrink-0 text-success" aria-label={`Succeeded in run #${sample.runNumber}`} />
          )}
          {sample?.status === "failed" && (
            <span title={sample.error}>
              <XCircle className="size-4 shrink-0 text-danger" aria-label={`Failed in run #${sample.runNumber}`} />
            </span>
          )}
        </div>

        {/* Needs-credentials warning (n8n-style) */}
        {needsCredentials && (
          <div className="mt-2 flex items-center gap-1.5 rounded-md bg-warn-bg px-2 py-1">
            <TriangleAlert className="size-3 shrink-0 text-warn" />
            <span className="text-[10px] font-medium text-warn">Sign in required</span>
          </div>
        )}

        {/* Show configured fields count */}
        {configEntries.length > 0 && (
          <div className="mt-2 pt-2 border-t border-border">
            <div className="text-[10px] text-faint">
              {configuredCount} / {configEntries.length} configured
            </div>
          </div>
        )}
      </div>

      {/* Output handle */}
      {spec.kind !== "output" && (
        <Handle
          type="source"
          position={Position.Bottom}
          className="!h-3 !w-3 !border-2 !border-border-strong !bg-surface"
        />
      )}
    </div>
  );
}
