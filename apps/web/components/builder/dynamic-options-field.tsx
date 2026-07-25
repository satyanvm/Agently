"use client";

import * as React from "react";
import { RefreshCw } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import {
  fetchPieceOptions,
  pieceCoordinates,
  type PieceOption,
} from "@/lib/piece-options";
import { CREDENTIAL_ID_KEY, type NodeField } from "./node-catalog";

/**
 * n8n-style two-mode control for a dynamic Activepieces prop on a node with a
 * selected credential: "From list" fetches the real options from the provider
 * via POST /api/pieces/options (lazily, cached per node-type+prop+credential);
 * "By ID" is the raw text input. Any fetch failure shows compactly and drops
 * to By ID — the form is never blocked. Values round-trip as raw config values.
 */

const optionsCache = new Map<string, PieceOption[]>();

export function DynamicOptionsField({
  field,
  value,
  onChange,
  nodeTypeId,
  config,
  credentialId,
}: {
  field: NodeField;
  value: unknown;
  onChange: (value: unknown) => void;
  nodeTypeId: string;
  config: Record<string, unknown>;
  credentialId: string;
}) {
  const coords = pieceCoordinates(nodeTypeId);
  const cacheKey = `${nodeTypeId}:${field.key}:${credentialId}`;

  const [mode, setMode] = React.useState<"list" | "id">("list");
  const [options, setOptions] = React.useState<PieceOption[] | null>(
    optionsCache.get(cacheKey) ?? null,
  );
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const load = React.useCallback(
    async (force = false) => {
      if (!coords) return;
      if (!force && optionsCache.has(cacheKey)) {
        setOptions(optionsCache.get(cacheKey)!);
        return;
      }
      setLoading(true);
      setError(null);
      // Refresher-dependent dropdowns see the node's other config values; the
      // reserved credential key stays out of piece props.
      const { [CREDENTIAL_ID_KEY]: _omit, ...props } = config;
      const result = await fetchPieceOptions({
        piece: coords.piece,
        actionOrTrigger: coords.actionOrTrigger,
        propKey: field.key,
        credentialId,
        authEnvKey: coords.authEnvKey,
        props,
      });
      setLoading(false);
      if (result.ok) {
        optionsCache.set(cacheKey, result.options);
        setOptions(result.options);
      } else {
        setError(result.error);
        setMode("id"); // never block the form on a failed lookup
      }
    },
    // config is intentionally read fresh on each call, not a dependency — a
    // keystroke in another field must not refetch options.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cacheKey, credentialId, field.key],
  );

  React.useEffect(() => {
    if (mode === "list" && options === null && !loading && !error) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, cacheKey]);

  if (!coords) return null;

  const valueKey = serialize(value);
  const hasValueInOptions = (options ?? []).some((o) => serialize(o.value) === valueKey);

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className="block text-[12px] font-medium text-fg">
          {field.label}
          {field.required && (
            <span className="text-danger ml-0.5" aria-hidden>
              *
            </span>
          )}
        </label>
        <div className="flex items-center gap-0.5 rounded-md border border-border p-0.5">
          <ModeButton active={mode === "list"} onClick={() => setMode("list")}>
            From list
          </ModeButton>
          <ModeButton active={mode === "id"} onClick={() => setMode("id")}>
            By ID
          </ModeButton>
        </div>
      </div>

      {mode === "id" ? (
        <Input
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.placeholder}
          className="text-[13px]"
        />
      ) : loading ? (
        <div className="h-9 flex items-center rounded-md border border-border bg-surface px-3 text-[12px] text-faint">
          Loading options…
        </div>
      ) : (
        <div className="flex items-center gap-1.5">
          <select
            value={hasValueInOptions ? valueKey : ""}
            onChange={(e) => {
              const picked = (options ?? []).find((o) => serialize(o.value) === e.target.value);
              if (picked) onChange(picked.value);
            }}
            className={cn(
              "w-full h-9 min-w-0 flex-1 rounded-md border border-border bg-surface px-3 text-[13px] text-fg",
              "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25",
            )}
          >
            <option value="" disabled>
              {value != null && value !== "" && !hasValueInOptions
                ? `Current: ${String(value)}`
                : "Select…"}
            </option>
            {(options ?? []).map((o, i) => (
              <option key={`${i}-${serialize(o.value)}`} value={serialize(o.value)}>
                {o.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => void load(true)}
            aria-label="Refresh options"
            title="Refresh options"
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-border text-faint transition-colors hover:bg-surface-2 hover:text-fg"
          >
            <RefreshCw className="size-3.5" />
          </button>
        </div>
      )}

      {error && mode === "id" && (
        <p className="mt-1 text-[11px] text-danger">Couldn’t load options: {error}</p>
      )}
      {field.help && !error && (
        <p className="mt-1 text-[11px] text-faint">{field.help}</p>
      )}
    </div>
  );
}

function ModeButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors",
        active ? "bg-accent/15 text-accent" : "text-faint hover:text-fg",
      )}
    >
      {children}
    </button>
  );
}

/** Stable string key for option values (they may be objects, e.g. channel refs). */
function serialize(v: unknown): string {
  if (typeof v === "string") return v;
  try {
    return JSON.stringify(v) ?? "";
  } catch {
    return String(v);
  }
}
