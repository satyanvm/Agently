"use client";

import * as React from "react";
import { KeyRound, Pencil, Plus, RefreshCw, Trash2, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import {
  createCredential,
  deleteCredential,
  updateCredential,
  type CredentialSummary,
} from "@/lib/credentials";
import { credentialTypeDef, type CredentialTypeDef } from "./credential-types";
import { useCredentials } from "./credentials-context";
import { DynamicField } from "./dynamic-field";
import type { NodeField } from "./node-catalog";

/**
 * n8n-style credential picker shown at the top of the node inspector for any
 * node whose spec carries a `credentialType` (contract §2/§6). Selecting a
 * credential stores its id under the reserved `__credentialId` config key via
 * `onSelect`; "+ Create new" / edit open a modal whose form is rendered from
 * credential-types.generated.json through the shared DynamicField renderer.
 */
export function CredentialSection({
  credentialType,
  selectedId,
  onSelect,
}: {
  credentialType: string;
  selectedId: string | undefined;
  onSelect: (id: string | undefined) => void;
}) {
  const typeDef = credentialTypeDef(credentialType);
  const { credentials, loading, error, refresh } = useCredentials();
  const [modal, setModal] = React.useState<
    { mode: "create" } | { mode: "edit"; cred: CredentialSummary } | null
  >(null);

  const ofType = React.useMemo(
    () => (credentials ?? []).filter((c) => c.type === credentialType),
    [credentials, credentialType],
  );
  const selected = ofType.find((c) => c.id === selectedId);
  // Only flag a dangling reference once the list actually loaded — never while
  // loading or when the API is unreachable.
  const dangling = Boolean(selectedId && credentials !== null && !selected);

  return (
    <div className="pb-4 border-b border-border">
      <div className="flex items-center gap-1.5 mb-2">
        <KeyRound className="size-3.5 text-muted" />
        <span className="text-[11px] font-semibold uppercase tracking-wider text-muted">
          Credentials
        </span>
      </div>

      <label className="block text-[12px] font-medium text-fg mb-1.5">
        {typeDef.label} account
        <span className="text-danger ml-0.5" aria-hidden>
          *
        </span>
      </label>

      {credentials === null && loading ? (
        <div className="text-[12px] text-faint py-1">Loading credentials…</div>
      ) : credentials === null && error ? (
        // API unreachable: non-blocking — the rest of the builder still works.
        <div className="rounded-md border border-warn/30 bg-warn-bg px-2.5 py-2">
          <div className="flex items-center gap-1.5 text-[11px] text-warn">
            <TriangleAlert className="size-3.5 shrink-0" />
            Couldn't load credentials
          </div>
          <button
            onClick={refresh}
            className="mt-1 inline-flex items-center gap-1 text-[11px] text-muted hover:text-fg transition-colors"
          >
            <RefreshCw className="size-3" /> Retry
          </button>
        </div>
      ) : ofType.length === 0 && !dangling ? (
        // Friendly empty state: nothing of this type yet.
        <div className="rounded-md border border-border bg-surface-2 px-3 py-3 text-center">
          <div className="text-[12px] text-muted mb-2">
            No {typeDef.label} credentials yet
          </div>
          <Button variant="secondary" size="sm" className="w-full" onClick={() => setModal({ mode: "create" })}>
            <Plus className="size-3.5" />
            Create credential
          </Button>
        </div>
      ) : (
        <>
          <div className="flex items-center gap-1.5">
            <select
              value={selected ? selected.id : ""}
              onChange={(e) => onSelect(e.target.value || undefined)}
              className={cn(
                "flex-1 min-w-0 h-9 rounded-md border bg-surface px-3 text-[13px] text-fg",
                "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25",
                dangling ? "border-warn/50" : "border-border",
              )}
            >
              <option value="">Select credential…</option>
              {ofType.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
            {selected && (
              <button
                onClick={() => setModal({ mode: "edit", cred: selected })}
                aria-label="Edit credential"
                title="Edit credential"
                className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-border text-muted transition-colors hover:bg-surface-2 hover:text-fg"
              >
                <Pencil className="size-3.5" />
              </button>
            )}
          </div>
          {dangling && (
            <p className="mt-1.5 flex items-center gap-1 text-[11px] text-warn">
              <TriangleAlert className="size-3 shrink-0" />
              The saved credential no longer exists — pick another.
            </p>
          )}
          <button
            onClick={() => setModal({ mode: "create" })}
            className="mt-1.5 inline-flex items-center gap-1 text-[12px] text-accent hover:text-accent-soft transition-colors"
          >
            <Plus className="size-3.5" /> Create new
          </button>
        </>
      )}

      {modal && (
        <CredentialModal
          typeDef={typeDef}
          existing={modal.mode === "edit" ? modal.cred : undefined}
          onClose={() => setModal(null)}
          onSaved={async (cred) => {
            await refresh();
            onSelect(cred.id);
            setModal(null);
          }}
          onDeleted={async (id) => {
            await refresh();
            if (id === selectedId) onSelect(undefined);
            setModal(null);
          }}
        />
      )}
    </div>
  );
}

/* ------------------------------ modal form ------------------------------ */

/**
 * Create/edit form rendered from the credential type's field definitions via
 * the shared DynamicField renderer, plus a Name field. Stored values are
 * write-only: on edit, fields whose key is in `setKeys` show a "(saved)"
 * placeholder and are only included in the PUT when the user types a new
 * value (the API merges values per-key).
 */
function CredentialModal({
  typeDef,
  existing,
  onClose,
  onSaved,
  onDeleted,
}: {
  typeDef: CredentialTypeDef;
  existing?: CredentialSummary;
  onClose: () => void;
  onSaved: (cred: CredentialSummary) => void | Promise<void>;
  onDeleted: (id: string) => void | Promise<void>;
}) {
  const [name, setName] = React.useState(existing?.name ?? `${typeDef.label} account`);
  // Only keys the user actually touched — untouched saved values are omitted
  // from the PUT so the API preserves them. On create, selects are seeded with
  // their first option since that's what the control displays.
  const [values, setValues] = React.useState<Record<string, unknown>>(() => {
    if (existing) return {};
    const seed: Record<string, unknown> = {};
    for (const f of typeDef.fields) {
      if (f.control === "select" && f.options?.length) seed[f.key] = f.options[0];
    }
    return seed;
  });
  const [busy, setBusy] = React.useState(false);
  const [apiError, setApiError] = React.useState<string | null>(null);

  const isEdit = Boolean(existing);
  const fields = typeDef.fields;

  const filled = (v: unknown) => v !== undefined && v !== null && v !== "";
  const canSave =
    name.trim() !== "" &&
    (isEdit ||
      fields.every(
        (f) => !f.required || f.control === "checkbox" || filled(values[f.key]),
      ));

  const savedPlaceholder = (f: NodeField): string | undefined => {
    if (!existing || !existing.setKeys.includes(f.key) || f.key in values) return undefined;
    return f.control === "secret" ? "•••••• (saved)" : "(saved — leave blank to keep)";
  };

  const handleSave = async () => {
    setBusy(true);
    setApiError(null);
    try {
      const typed: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(values)) {
        if (filled(v) || typeof v === "boolean") typed[k] = v;
      }
      const cred = existing
        ? await updateCredential(existing.id, {
            name: name.trim(),
            ...(Object.keys(typed).length > 0 ? { values: typed } : {}),
          })
        : await createCredential({ name: name.trim(), type: typeDef.id, values: typed });
      await onSaved(cred);
    } catch (e) {
      setApiError(e instanceof Error ? e.message : "Failed to save credential");
      setBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!existing) return;
    if (!window.confirm(`Delete credential "${existing.name}"? Nodes using it will need a new one.`)) {
      return;
    }
    setBusy(true);
    setApiError(null);
    try {
      await deleteCredential(existing.id);
      await onDeleted(existing.id);
    } catch (e) {
      setApiError(e instanceof Error ? e.message : "Failed to delete credential");
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()} className="max-w-[440px]">
      <DialogHeader
        title={isEdit ? `Edit ${typeDef.label} credential` : `Create ${typeDef.label} credential`}
        subtitle={
          isEdit
            ? "Saved values stay hidden — retype a field to replace it."
            : "Stored securely; values are never shown again after saving."
        }
        onClose={onClose}
      />

      <div className="max-h-[55vh] overflow-y-auto px-5 py-4 space-y-4">
        <div>
          <label className="block text-[12px] font-medium text-fg mb-1.5">
            Name
            <span className="text-danger ml-0.5" aria-hidden>
              *
            </span>
          </label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={`${typeDef.label} account`}
            className="text-[13px]"
          />
        </div>

        {fields.map((f) => (
          <DynamicField
            key={f.key}
            field={f}
            value={values[f.key]}
            onChange={(v) => setValues((prev) => ({ ...prev, [f.key]: v }))}
            placeholder={savedPlaceholder(f)}
          />
        ))}

        {fields.length === 0 && (
          <p className="text-[11px] text-faint">
            No field definitions are available for this credential type yet — the
            catalog may still be regenerating.
          </p>
        )}

        {apiError && <p className="text-[11px] text-danger break-words">{apiError}</p>}
      </div>

      <div className="flex items-center justify-between gap-2 border-t border-border px-5 py-3">
        <div>
          {isEdit && (
            <Button variant="danger" size="sm" onClick={handleDelete} disabled={busy}>
              <Trash2 className="size-3.5" />
              Delete
            </Button>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" size="sm" onClick={handleSave} disabled={busy || !canSave}>
            {busy ? "Saving…" : isEdit ? "Save changes" : "Create"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
