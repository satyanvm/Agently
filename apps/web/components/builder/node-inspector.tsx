"use client";

import * as React from "react";
import { Trash2, X } from "lucide-react";
import type { Node } from "@xyflow/react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { findNodeSpec, nodeFields, type NodeField } from "./node-catalog";
import { cn } from "@/lib/utils";
import type { NodeData, WorkflowNodeType } from "./workflow-builder";

interface NodeInspectorProps {
  node: WorkflowNodeType | undefined;
  onUpdateConfig: (nodeId: string, config: Record<string, unknown>) => void;
  onDelete: () => void;
}

export function NodeInspector({ node, onUpdateConfig, onDelete }: NodeInspectorProps) {
  if (!node) {
    return (
      <div className="w-[320px] border-l border-border bg-surface flex flex-col h-full">
        <div className="flex-1 flex items-center justify-center p-8 text-center">
          <div>
            <div className="text-[13px] font-medium text-muted mb-1">No node selected</div>
            <div className="text-[11px] text-faint">
              Click on a node to configure it
            </div>
          </div>
        </div>
      </div>
    );
  }

  const spec = findNodeSpec(node.data.typeId);
  if (!spec) return null;

  const Icon = spec.icon;
  const fields = nodeFields(node.data.typeId);

  const updateField = (key: string, value: unknown) => {
    onUpdateConfig(node.id, {
      ...node.data.config,
      [key]: value,
    });
  };

  return (
    <div className="w-[320px] border-l border-border bg-surface flex flex-col h-full">
      {/* Header */}
      <div className="px-4 py-4 border-b border-border">
        <div className="flex items-start gap-3">
          <div className={cn("flex h-9 w-9 shrink-0 items-center justify-center rounded-md", spec.toneBg)}>
            <Icon className={cn("size-5", spec.tone)} />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="text-[13px] font-semibold text-fg">{node.data.label}</h3>
            <p className="text-[11px] text-muted mt-0.5">{spec.description}</p>
          </div>
        </div>
      </div>

      {/* Configuration fields */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {fields.length === 0 ? (
          <div className="text-[12px] text-faint text-center py-8">
            No configuration needed
          </div>
        ) : (
          fields.map((field) => (
            <NodeField
              key={field.key}
              field={field}
              value={node.data.config[field.key]}
              onChange={(value) => updateField(field.key, value)}
            />
          ))
        )}
      </div>

      {/* Footer actions */}
      <div className="px-4 py-3 border-t border-border">
        <Button
          variant="danger"
          size="sm"
          className="w-full"
          onClick={onDelete}
        >
          <Trash2 className="size-4" />
          Delete node
        </Button>
      </div>
    </div>
  );
}

function NodeField({
  field,
  value,
  onChange,
}: {
  field: NodeField;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  return (
    <div>
      <label className="block text-[12px] font-medium text-fg mb-1.5">
        {field.label}
      </label>
      {field.control === "text" && (
        <Input
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.placeholder}
          className="text-[13px]"
        />
      )}
      {field.control === "textarea" && (
        <textarea
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.placeholder}
          rows={4}
          className={cn(
            "w-full rounded-md border border-border bg-surface px-3 py-2 text-[13px] text-fg placeholder:text-faint",
            "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25",
            "resize-none font-mono"
          )}
        />
      )}
      {field.control === "select" && field.options && (
        <select
          value={String(value ?? field.options[0])}
          onChange={(e) => onChange(e.target.value)}
          className={cn(
            "w-full h-9 rounded-md border border-border bg-surface px-3 text-[13px] text-fg",
            "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25"
          )}
        >
          {field.options.map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      )}
      {field.help && (
        <p className="mt-1 text-[11px] text-faint">{field.help}</p>
      )}
    </div>
  );
}
