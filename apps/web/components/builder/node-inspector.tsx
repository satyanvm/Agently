"use client";

import * as React from "react";
import { Database, Loader2, Play, Settings2, SlidersHorizontal, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { CREDENTIAL_ID_KEY, findNodeSpec, nodeFields } from "./node-catalog";
import { CredentialSection } from "./credential-section";
import { DynamicField } from "./dynamic-field";
import { DynamicOptionsField } from "./dynamic-options-field";
import { NodeDataPanel, UpstreamFields } from "./node-data-panel";
import { NodeSettingsPanel } from "./node-settings-panel";
import {
  PINNED_KEY,
  REF_MIME,
  SETTINGS_KEY,
  type NodeSamples,
  type NodeSettings,
} from "@/lib/builder-debug";
import { cn } from "@/lib/utils";
import type { WorkflowNodeType } from "./workflow-builder";

type Tab = "params" | "data" | "settings";

interface NodeInspectorProps {
  node: WorkflowNodeType | undefined;
  /** Direct dependencies (incoming edges) — the data this node can map. */
  deps: { key: string; label: string; pinned?: Record<string, unknown> }[];
  samples: NodeSamples | null;
  onUpdateConfig: (nodeId: string, config: Record<string, unknown>) => void;
  onDelete: () => void;
  onRunStep: (nodeId: string) => void;
  /** Node id whose step run is in flight, if any. */
  stepRunning: string | null;
  busy: boolean;
}

export function NodeInspector({
  node,
  deps,
  samples,
  onUpdateConfig,
  onDelete,
  onRunStep,
  stepRunning,
  busy,
}: NodeInspectorProps) {
  const [tab, setTab] = React.useState<Tab>("params");

  if (!node) {
    return (
      <div className="w-[360px] border-l border-border bg-surface flex flex-col h-full">
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
  // Reserved keys (__credentialId, __pinned, __settings) have their own UI —
  // never rendered as normal config fields (contract §6).
  const fields = nodeFields(node.data.typeId).filter((f) => f.key !== CREDENTIAL_ID_KEY);

  const updateField = (key: string, value: unknown) => {
    onUpdateConfig(node.id, {
      ...node.data.config,
      [key]: value,
    });
  };
  const setReserved = (key: string, value: unknown) => {
    const next = { ...node.data.config };
    if (value === undefined) delete next[key];
    else next[key] = value;
    onUpdateConfig(node.id, next);
  };

  const rawCredentialId = node.data.config[CREDENTIAL_ID_KEY];
  const selectedCredentialId =
    typeof rawCredentialId === "string" && rawCredentialId !== "" ? rawCredentialId : undefined;

  const selectCredential = (id: string | undefined) => setReserved(CREDENTIAL_ID_KEY, id);

  const pinned = node.data.config[PINNED_KEY] as Record<string, unknown> | undefined;
  const settings = (node.data.config[SETTINGS_KEY] as NodeSettings | undefined) ?? {};
  const running = stepRunning === node.id;

  return (
    <div className="w-[360px] border-l border-border bg-surface flex flex-col h-full">
      {/* Header */}
      <div className="px-4 pt-4 pb-3 border-b border-border space-y-3">
        <div className="flex items-start gap-3">
          <div className={cn("flex h-9 w-9 shrink-0 items-center justify-center rounded-md", spec.toneBg)}>
            <Icon className={cn("size-5", spec.tone)} />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="text-[13px] font-semibold text-fg">{node.data.label}</h3>
            <p className="text-[11px] text-muted mt-0.5">{spec.description}</p>
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => onRunStep(node.id)}
            disabled={busy}
            title="Run only this node, reusing upstream nodes' last outputs"
          >
            {running ? <Loader2 className="animate-spin" /> : <Play />}
            {running ? "Running" : "Run step"}
          </Button>
        </div>
        <Segmented<Tab>
          size="sm"
          className="w-full [&>button]:flex-1 [&>button]:justify-center"
          value={tab}
          onChange={setTab}
          options={[
            { value: "params", label: "Parameters", icon: <SlidersHorizontal className="size-3.5" /> },
            { value: "data", label: "Data", icon: <Database className="size-3.5" /> },
            { value: "settings", label: "Settings", icon: <Settings2 className="size-3.5" /> },
          ]}
        />
      </div>

      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {tab === "params" && (
          <>
            {spec.credentialType && (
              <CredentialSection
                key={`${node.id}:${spec.credentialType}`}
                credentialType={spec.credentialType}
                selectedId={selectedCredentialId}
                onSelect={selectCredential}
              />
            )}
            {fields.length > 0 && <UpstreamFields key={node.id} deps={deps} samples={samples} />}
            {fields.length === 0 ? (
              <div className="text-[12px] text-faint text-center py-8">
                No configuration needed
              </div>
            ) : (
              fields.map((field) => (
                <MapDropTarget
                  key={field.key}
                  value={node.data.config[field.key]}
                  onMap={(v) => updateField(field.key, v)}
                >
                  {
                    // Dynamic pieces props with a credential selected get the live
                    // From-list / By-ID control; everything else the plain renderer.
                    field.dynamic && selectedCredentialId && node.data.typeId.startsWith("pieces.") ? (
                      <DynamicOptionsField
                        field={field}
                        value={node.data.config[field.key]}
                        onChange={(value) => updateField(field.key, value)}
                        nodeTypeId={node.data.typeId}
                        config={node.data.config}
                        credentialId={selectedCredentialId}
                      />
                    ) : (
                      <DynamicField
                        field={field}
                        value={node.data.config[field.key]}
                        onChange={(value) => updateField(field.key, value)}
                      />
                    )
                  }
                </MapDropTarget>
              ))
            )}
          </>
        )}

        {tab === "data" && (
          <NodeDataPanel
            nodeKey={node.id}
            deps={deps}
            samples={samples}
            pinned={pinned}
            onPin={(o) => setReserved(PINNED_KEY, o)}
            onUnpin={() => setReserved(PINNED_KEY, undefined)}
          />
        )}

        {tab === "settings" && (
          <NodeSettingsPanel
            settings={settings}
            onChange={(s) => {
              const clean = Object.fromEntries(Object.entries(s).filter(([, v]) => v !== undefined && v !== false));
              setReserved(SETTINGS_KEY, Object.keys(clean).length > 0 ? clean : undefined);
            }}
          />
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

/**
 * Accepts output fields dragged from the Data tab. Dropped on a text box the
 * browser inserts the reference at the drop point; anywhere else on the field
 * (label, dropdown) the reference is appended to the current value.
 */
function MapDropTarget({
  value,
  onMap,
  children,
}: {
  value: unknown;
  onMap: (v: string) => void;
  children: React.ReactNode;
}) {
  const [over, setOver] = React.useState(false);
  const isRef = (e: React.DragEvent) => e.dataTransfer.types.includes(REF_MIME);
  return (
    <div
      onDragOver={(e) => {
        if (!isRef(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        setOver(false);
        if (!isRef(e)) return;
        const t = e.target as HTMLElement;
        const textBox =
          t instanceof HTMLTextAreaElement ||
          (t instanceof HTMLInputElement && !["checkbox", "radio", "number"].includes(t.type));
        if (textBox) return; // native insert at the caret → the field's onChange
        e.preventDefault();
        const ref = e.dataTransfer.getData(REF_MIME);
        const cur = typeof value === "string" ? value : "";
        onMap(cur ? `${cur} ${ref}` : ref);
      }}
      className={cn("rounded-md transition-shadow", over && "ring-2 ring-accent/40 ring-offset-2 ring-offset-surface")}
    >
      {children}
    </div>
  );
}
