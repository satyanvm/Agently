"use client";

import * as React from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { CREDENTIAL_ID_KEY, findNodeSpec, nodeFields } from "./node-catalog";
import { CredentialSection } from "./credential-section";
import { DynamicField } from "./dynamic-field";
import { DynamicOptionsField } from "./dynamic-options-field";
import { cn } from "@/lib/utils";
import type { WorkflowNodeType } from "./workflow-builder";

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
  // The reserved __credentialId key is handled by the credential section only —
  // never rendered as a normal config field (contract §6).
  const fields = nodeFields(node.data.typeId).filter((f) => f.key !== CREDENTIAL_ID_KEY);

  const updateField = (key: string, value: unknown) => {
    onUpdateConfig(node.id, {
      ...node.data.config,
      [key]: value,
    });
  };

  const rawCredentialId = node.data.config[CREDENTIAL_ID_KEY];
  const selectedCredentialId =
    typeof rawCredentialId === "string" && rawCredentialId !== "" ? rawCredentialId : undefined;

  const selectCredential = (id: string | undefined) => {
    const next = { ...node.data.config };
    if (id) next[CREDENTIAL_ID_KEY] = id;
    else delete next[CREDENTIAL_ID_KEY];
    onUpdateConfig(node.id, next);
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

      {/* Credentials first (n8n-style), then configuration fields */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {spec.credentialType && (
          <CredentialSection
            key={`${node.id}:${spec.credentialType}`}
            credentialType={spec.credentialType}
            selectedId={selectedCredentialId}
            onSelect={selectCredential}
          />
        )}
        {fields.length === 0 ? (
          <div className="text-[12px] text-faint text-center py-8">
            No configuration needed
          </div>
        ) : (
          fields.map((field) =>
            // Dynamic pieces props with a credential selected get the live
            // From-list / By-ID control; everything else the plain renderer.
            field.dynamic && selectedCredentialId && node.data.typeId.startsWith("pieces.") ? (
              <DynamicOptionsField
                key={field.key}
                field={field}
                value={node.data.config[field.key]}
                onChange={(value) => updateField(field.key, value)}
                nodeTypeId={node.data.typeId}
                config={node.data.config}
                credentialId={selectedCredentialId}
              />
            ) : (
              <DynamicField
                key={field.key}
                field={field}
                value={node.data.config[field.key]}
                onChange={(value) => updateField(field.key, value)}
              />
            ),
          )
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
