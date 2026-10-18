"use client";

import * as React from "react";
import { Save } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  apiErrorMessage,
  fetchBrand,
  rememberName,
  rememberedName,
  saveBrand,
  type BrandContext,
} from "@/lib/marketing";
import { timeAgo } from "@/lib/utils";

const TEXTAREA =
  "w-full rounded-md border border-border bg-surface px-3 py-2.5 text-[13px] leading-relaxed text-fg placeholder:text-faint/70 transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25";

type Field = "voice" | "productFacts" | "positioning" | "avoidClaims";

const FIELDS: { key: Field; label: string; help: string; placeholder: string }[] = [
  {
    key: "voice",
    label: "Brand voice",
    help: "How you sound. Drafts follow this tone.",
    placeholder: "Plain and direct. Short sentences. Confident, never smug. No exclamation marks.",
  },
  {
    key: "productFacts",
    label: "Product facts",
    help: "What's true about your product: features, prices, numbers drafts may quote.",
    placeholder: "Starter is $12/seat/month. SOC 2 Type II. Integrates with Slack, HubSpot and Salesforce.",
  },
  {
    key: "positioning",
    label: "Positioning",
    help: "Who you're for and why you win, especially against the competitors you watch.",
    placeholder: "For B2B teams of 5–50 who want X without Y. Unlike Acme, we…",
  },
  {
    key: "avoidClaims",
    label: "Claims to avoid",
    help: "Things drafts must never say.",
    placeholder: "Never claim to be the cheapest. Don't name customers without permission. No “best in class”.",
  },
];

export default function BrandPage() {
  const [brand, setBrand] = React.useState<BrandContext | null>(null);
  const [form, setForm] = React.useState<Record<Field, string>>({ voice: "", productFacts: "", positioning: "", avoidClaims: "" });
  const [name, setName] = React.useState("");
  const [state, setState] = React.useState<"idle" | "saving" | "saved">("idle");
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    setName(rememberedName());
    fetchBrand()
      .then((b) => {
        setBrand(b);
        setForm({ voice: b.voice, productFacts: b.productFacts, positioning: b.positioning, avoidClaims: b.avoidClaims });
      })
      .catch((e) => setError(apiErrorMessage(e)));
  }, []);

  const dirty =
    brand !== null && FIELDS.some((f) => form[f.key] !== (brand[f.key] ?? ""));

  async function save() {
    setState("saving");
    setError("");
    try {
      if (name.trim()) rememberName(name.trim());
      const saved = await saveBrand({ ...form, updatedBy: name.trim() });
      setBrand(saved);
      setForm({ voice: saved.voice, productFacts: saved.productFacts, positioning: saved.positioning, avoidClaims: saved.avoidClaims });
      setState("saved");
    } catch (e) {
      setError(apiErrorMessage(e));
      setState("idle");
    }
  }

  return (
    <>
      <TopBar crumbs={[{ label: "Brand" }]} />
      <PageContainer className="max-w-[880px]">
        <PageTitle
          title="Brand"
          subtitle="How drafts should sound and what they may claim. AI drafting steps that use brand context are given this text."
        />
        {error && <Card className="mb-4 border-danger/30 px-4 py-3 text-[13px] text-danger">{error}</Card>}
        <Card className="space-y-5 p-5">
          {FIELDS.map((f) => (
            <label key={f.key} className="block">
              <span className="block text-[13px] font-semibold text-fg">{f.label}</span>
              <span className="mb-1.5 block text-[12px] text-muted">{f.help}</span>
              <textarea
                value={form[f.key]}
                onChange={(e) => {
                  setForm({ ...form, [f.key]: e.target.value });
                  setState("idle");
                }}
                rows={4}
                placeholder={f.placeholder}
                className={TEXTAREA}
                disabled={brand === null}
              />
            </label>
          ))}
          <div className="flex flex-wrap items-end gap-3 border-t border-border pt-4">
            <label className="block w-56">
              <span className="mb-1 block text-[12px] font-medium text-muted">Your name</span>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Shown as last editor" />
            </label>
            <Button variant="primary" onClick={save} disabled={brand === null || state === "saving" || !dirty}>
              <Save /> {state === "saving" ? "Saving…" : "Save"}
            </Button>
            <span className="text-[12px] text-faint">
              {state === "saved"
                ? "Saved."
                : brand?.updatedAt
                  ? `Last saved ${timeAgo(brand.updatedAt)}${brand.updatedBy ? ` by ${brand.updatedBy}` : ""}.`
                  : "Not set yet."}
            </span>
          </div>
        </Card>
      </PageContainer>
    </>
  );
}
