"use client";

import * as React from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { findNodeSpec } from "./node-catalog";
import { cn } from "@/lib/utils";
import type { WorkflowNodeType } from "./workflow-builder";

export function WorkflowNode({ data, selected }: NodeProps<WorkflowNodeType>) {
  const spec = findNodeSpec(data.typeId);
  if (!spec) return null;

  const Icon = spec.icon;

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
            <div className="text-[11px] text-muted mt-0.5">{spec.kind}</div>
          </div>
        </div>

        {/* Show configured fields count */}
        {Object.keys(data.config).length > 0 && (
          <div className="mt-2 pt-2 border-t border-border">
            <div className="text-[10px] text-faint">
              {Object.values(data.config).filter((v) => v !== "").length} / {Object.keys(data.config).length} configured
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
