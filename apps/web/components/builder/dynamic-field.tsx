"use client";

import * as React from "react";
import { Eye, EyeOff } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import type { NodeField } from "./node-catalog";

/**
 * The single dynamic field renderer (docs/credentials-contract.md §1): maps a
 * NodeField's `control` to a component. Used by BOTH the node inspector's
 * config form and the credential create/edit forms, so field behavior stays
 * identical everywhere.
 *
 *   text     → single-line input (infers type=email / type=url from key/label)
 *   secret   → password input with show/hide toggle
 *   textarea → multi-line input
 *   number   → numeric input
 *   checkbox → checkbox (boolean value)
 *   select   → select over `options`
 *   json     → monospace textarea with JSON parse validation
 *
 * `dynamic: true` fields (Activepieces dropdown/dynamic props) render as text
 * with the help line styled as a hint that a raw ID is expected.
 */
export function DynamicField({
  field,
  value,
  onChange,
  placeholder,
}: {
  field: NodeField;
  value: unknown;
  onChange: (value: unknown) => void;
  /** Overrides field.placeholder (e.g. "•••••• (saved)" for stored secrets). */
  placeholder?: string;
}) {
  const hint = placeholder ?? field.placeholder;

  // Checkbox gets an inline label layout; everything else a label-above layout.
  if (field.control === "checkbox") {
    return (
      <div>
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={value === true}
            onChange={(e) => onChange(e.target.checked)}
            className="h-4 w-4 rounded border-border accent-accent"
          />
          <span className="text-[12px] font-medium text-fg">
            {field.label}
            {field.required && <RequiredMark />}
          </span>
        </label>
        <FieldHelp field={field} />
      </div>
    );
  }

  return (
    <div>
      <label className="block text-[12px] font-medium text-fg mb-1.5">
        {field.label}
        {field.required && <RequiredMark />}
      </label>
      <FieldControl field={field} value={value} onChange={onChange} placeholder={hint} />
      <FieldHelp field={field} />
    </div>
  );
}

/** n8n-style required asterisk. */
function RequiredMark() {
  return (
    <span className="text-danger ml-0.5" aria-hidden>
      *
    </span>
  );
}

function FieldHelp({ field }: { field: NodeField }) {
  if (field.dynamic) {
    // Dynamic (Activepieces dropdown/dynamic) props: no live authenticated
    // lookup here — the help line becomes a hint that a raw ID is expected.
    return (
      <p className="mt-1 text-[11px] italic text-faint">
        {field.help ?? "Enter the raw ID — dynamic options can't be listed in the builder."}
      </p>
    );
  }
  if (!field.help) return null;
  return <p className="mt-1 text-[11px] text-faint">{field.help}</p>;
}

/** Infer a more specific input type for plain text fields (contract §1 sugar). */
function inferTextType(field: NodeField): string {
  const hay = `${field.key} ${field.label}`;
  if (/email/i.test(hay)) return "email";
  if (/url|link/i.test(hay)) return "url";
  return "text";
}

const TEXTAREA_CLASSES = cn(
  "w-full rounded-md border border-border bg-surface px-3 py-2 text-[13px] text-fg placeholder:text-faint",
  "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25",
  "resize-none font-mono",
);

function FieldControl({
  field,
  value,
  onChange,
  placeholder,
}: {
  field: NodeField;
  value: unknown;
  onChange: (value: unknown) => void;
  placeholder?: string;
}) {
  switch (field.control) {
    case "secret":
      return <SecretInput value={value} onChange={onChange} placeholder={placeholder} />;

    case "textarea":
      return (
        <textarea
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          rows={4}
          className={TEXTAREA_CLASSES}
        />
      );

    case "number":
      return (
        <Input
          type="number"
          value={value === "" || value == null ? "" : String(value)}
          onChange={(e) => {
            const raw = e.target.value;
            onChange(raw === "" ? "" : Number(raw));
          }}
          placeholder={placeholder}
          className="text-[13px]"
        />
      );

    case "select":
      return (
        <select
          value={String(value ?? field.options?.[0] ?? "")}
          onChange={(e) => onChange(e.target.value)}
          className={cn(
            "w-full h-9 rounded-md border border-border bg-surface px-3 text-[13px] text-fg",
            "transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25",
          )}
        >
          {(field.options ?? []).map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      );

    case "json":
      return <JsonInput value={value} onChange={onChange} placeholder={placeholder} />;

    // "text", dynamic:true fields, and any unknown future control fall back to
    // a plain text input so a newer generated catalog never breaks the form.
    default:
      return (
        <Input
          type={inferTextType(field)}
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          className="text-[13px]"
        />
      );
  }
}

/** Password input with a show/hide toggle. Values are write-only server-side. */
function SecretInput({
  value,
  onChange,
  placeholder,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  placeholder?: string;
}) {
  const [visible, setVisible] = React.useState(false);
  return (
    <div className="relative">
      <Input
        type={visible ? "text" : "password"}
        value={String(value ?? "")}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete="off"
        className="text-[13px] pr-9"
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? "Hide value" : "Show value"}
        className="absolute right-1.5 top-1/2 -translate-y-1/2 flex h-6 w-6 items-center justify-center rounded text-faint transition-colors hover:bg-surface-2 hover:text-fg"
      >
        {visible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
      </button>
    </div>
  );
}

/** Monospace textarea storing a JSON string, with subtle parse-error state. */
function JsonInput({
  value,
  onChange,
  placeholder,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  placeholder?: string;
}) {
  const text = String(value ?? "");
  let error: string | null = null;
  if (text.trim() !== "") {
    try {
      JSON.parse(text);
    } catch {
      error = "Invalid JSON";
    }
  }
  return (
    <div>
      <textarea
        value={text}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? "{ }"}
        rows={4}
        spellCheck={false}
        className={cn(TEXTAREA_CLASSES, error && "border-danger/60 focus:border-danger/60 focus:ring-danger/20")}
      />
      {error && <p className="mt-1 text-[11px] text-danger">{error}</p>}
    </div>
  );
}
