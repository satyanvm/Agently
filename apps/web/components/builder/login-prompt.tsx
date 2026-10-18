"use client";

import * as React from "react";
import { CheckCircle2, KeyRound, LogIn, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import type { CredentialSummary } from "@/lib/credentials";
import { CredentialModal } from "./credential-section";
import { credentialTypeDef, isLoginType } from "./credential-types";
import { CREDENTIAL_ID_KEY, findNodeSpec } from "./node-catalog";
import type { WorkflowNodeType } from "./workflow-builder";

/**
 * Whether a node still needs a login/credential before it can run: its spec
 * has a credentialType and its __credentialId is unset or points at a deleted
 * credential. Unknown (null) credentials — still loading or API down — never
 * count as missing, so nothing nags while the list is unavailable.
 */
export function nodeNeedsLogin(
  node: WorkflowNodeType,
  credentials: CredentialSummary[] | null,
): boolean {
  const spec = findNodeSpec(node.data.typeId);
  if (!spec?.credentialType) return false;
  const id = node.data.config[CREDENTIAL_ID_KEY];
  if (typeof id !== "string" || id === "") return true;
  return credentials !== null && !credentials.some((c) => c.id === id);
}

/**
 * The "sign in" modal for one node, opened automatically by the builder when a
 * node that needs a login lands on the canvas (or from the run gate below).
 * Saving hands the new credential id back so the builder can attach it to
 * every node of the same type that is still missing one.
 */
export function NodeLoginModal({
  node,
  onConnected,
  onClose,
}: {
  node: WorkflowNodeType;
  onConnected: (credentialType: string, credentialId: string) => void | Promise<void>;
  onClose: () => void;
}) {
  const credentialType = findNodeSpec(node.data.typeId)?.credentialType;
  if (!credentialType) return null;
  return (
    <CredentialModal
      typeDef={credentialTypeDef(credentialType)}
      onClose={onClose}
      onSaved={(id) => onConnected(credentialType, id)}
    />
  );
}

/**
 * Shown when "Test run" is pressed while some nodes still need a login. Lists
 * one row per credential type (several nodes can share an account) with a
 * sign-in button; once everything is connected the primary action runs.
 */
export function RunLoginGate({
  missing,
  onSignIn,
  onRun,
  onClose,
}: {
  missing: WorkflowNodeType[];
  onSignIn: (node: WorkflowNodeType) => void;
  onRun: () => void;
  onClose: () => void;
}) {
  // Group by credential type: one sign-in covers every node that uses it.
  const groups = React.useMemo(() => {
    const byType = new Map<string, WorkflowNodeType[]>();
    for (const n of missing) {
      const t = findNodeSpec(n.data.typeId)?.credentialType;
      if (!t) continue;
      byType.set(t, [...(byType.get(t) ?? []), n]);
    }
    return [...byType.entries()].map(([type, nodes]) => ({ def: credentialTypeDef(type), nodes }));
  }, [missing]);

  const done = groups.length === 0;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()} className="max-w-[460px]">
      <DialogHeader
        title={done ? "All accounts connected" : "Sign in before running"}
        subtitle={
          done
            ? "Every node has the access it needs."
            : "These steps act on your accounts, so they need you to sign in first."
        }
        onClose={onClose}
      />
      <div className="px-5 py-4 space-y-2 max-h-[50vh] overflow-y-auto">
        {done ? (
          <div className="flex items-center gap-2 rounded-md bg-success-bg px-3 py-2.5 text-[13px] text-success">
            <CheckCircle2 className="size-4" /> Ready to run
          </div>
        ) : (
          groups.map(({ def, nodes }) => (
            <div
              key={def.id}
              className="flex items-center gap-3 rounded-md border border-border bg-surface-2 px-3 py-2.5"
            >
              <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-accent-bg">
                <KeyRound className="size-4 text-accent" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-[13px] font-medium text-fg truncate">{def.label}</div>
                <div className="text-[11px] text-muted truncate">
                  Used by {nodes.map((n) => n.data.label).join(", ")}
                </div>
              </div>
              <Button size="sm" variant="primary" onClick={() => nodes[0] && onSignIn(nodes[0])}>
                <LogIn className="size-3.5" />
                {isLoginType(def) ? "Sign in" : "Connect"}
              </Button>
            </div>
          ))
        )}
      </div>
      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
        {!done && (
          <Button variant="ghost" size="sm" onClick={onRun}>
            Run anyway
          </Button>
        )}
        <Button variant="primary" size="sm" onClick={onRun} disabled={!done}>
          <Play className="size-3.5" />
          Run now
        </Button>
      </div>
    </Dialog>
  );
}
