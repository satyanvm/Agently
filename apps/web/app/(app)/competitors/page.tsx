"use client";

import * as React from "react";
import { Plus, Trash2, Pencil, Archive, ExternalLink, Radar } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle, SectionHeading } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogHeader } from "@/components/ui/dialog";
import {
  PAGE_KINDS,
  apiErrorMessage,
  archiveCompetitor,
  createCompetitor,
  fetchCompetitors,
  fetchSignals,
  fetchSnapshots,
  updateCompetitor,
  type Competitor,
  type CompetitorInput,
  type CompetitorPage,
  type Signal,
  type SnapshotSummary,
} from "@/lib/marketing";
import { timeAgo } from "@/lib/utils";

const SELECT =
  "h-9 rounded-md border border-border bg-surface px-2 text-[13px] text-fg focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25";

export default function CompetitorsPage() {
  const [items, setItems] = React.useState<Competitor[] | null>(null);
  const [snaps, setSnaps] = React.useState<Record<string, SnapshotSummary[]>>({});
  const [signals, setSignals] = React.useState<Signal[]>([]);
  const [error, setError] = React.useState("");
  const [editing, setEditing] = React.useState<Competitor | "new" | null>(null);

  const load = React.useCallback(async () => {
    try {
      const [list, sigs] = await Promise.all([fetchCompetitors(), fetchSignals({ limit: 30 })]);
      setItems(list);
      setSignals(sigs);
      const bySnap = await Promise.all(list.map(async (c) => [c.id, await fetchSnapshots(c.id)] as const));
      setSnaps(Object.fromEntries(bySnap));
      setError("");
    } catch (e) {
      setError(apiErrorMessage(e));
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  async function archive(c: Competitor) {
    if (!window.confirm(`Stop watching ${c.name}? Its history is kept.`)) return;
    try {
      await archiveCompetitor(c.id);
      await load();
    } catch (e) {
      setError(apiErrorMessage(e));
    }
  }

  const names = Object.fromEntries((items ?? []).map((c) => [c.id, c.name]));

  return (
    <>
      <TopBar
        crumbs={[{ label: "Competitors" }]}
        actions={
          <Button variant="primary" size="sm" onClick={() => setEditing("new")}>
            <Plus /> Add competitor
          </Button>
        }
      />
      <PageContainer>
        <PageTitle
          title="Competitors"
          subtitle="The companies your playbooks watch. Each page is checked when a playbook runs “Watch competitors”, and every change is kept as a signal."
        />
        {error && <Card className="mb-4 border-danger/30 px-4 py-3 text-[13px] text-danger">{error}</Card>}

        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_340px]">
          <div className="min-w-0 space-y-3">
            {items === null && !error && <p className="text-sm text-faint">Loading…</p>}
            {items?.length === 0 && (
              <Card className="px-5 py-10 text-center text-[13px] text-muted">
                No competitors yet. Add one with its pricing, changelog, blog and careers pages.
              </Card>
            )}
            {items?.map((c) => {
              const versions = snaps[c.id] ?? [];
              return (
                <Card key={c.id} className="p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-[14px] font-semibold text-fg">{c.name}</div>
                      {c.website && (
                        <a href={c.website} target="_blank" rel="noreferrer" className="text-[12px] text-accent-soft hover:underline">
                          {c.website}
                        </a>
                      )}
                      {c.notes && <p className="mt-1 text-[12.5px] text-muted">{c.notes}</p>}
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Button variant="ghost" size="icon-sm" onClick={() => setEditing(c)} aria-label={`Edit ${c.name}`}>
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon-sm" onClick={() => archive(c)} aria-label={`Archive ${c.name}`}>
                        <Archive />
                      </Button>
                    </div>
                  </div>
                  <ul className="mt-3 divide-y divide-border rounded-md border border-border">
                    {c.pages.length === 0 && <li className="px-3 py-2 text-[12.5px] text-faint">No pages yet.</li>}
                    {c.pages.map((p) => {
                      const mine = versions.filter((v) => v.url === p.url);
                      const latest = mine[0];
                      return (
                        <li key={p.url} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-[12.5px]">
                          <Badge size="sm" variant="neutral">{p.kind}</Badge>
                          <a href={p.url} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate text-fg hover:underline">
                            {p.url}
                          </a>
                          {p.render === "browser" && <span className="text-faint">browser</span>}
                          <span className="text-faint">
                            {latest
                              ? `checked ${timeAgo(latest.lastSeenAt)} · ${mine.length} version${mine.length === 1 ? "" : "s"}`
                              : "not checked yet"}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                </Card>
              );
            })}
          </div>

          <Card className="p-4">
            <SectionHeading>Recent signals</SectionHeading>
            {signals.length === 0 ? (
              <p className="text-[12.5px] text-faint">Nothing detected yet.</p>
            ) : (
              <ul className="space-y-3">
                {signals.map((s) => (
                  <li key={s.id} className="text-[12.5px]">
                    <div className="flex items-center gap-1.5 font-medium text-fg">
                      <Radar className="size-3.5 shrink-0 text-success" />
                      <span className="min-w-0">{s.title}</span>
                    </div>
                    <div className="text-faint">
                      {s.source.replace("_", " ")}
                      {s.competitorId && names[s.competitorId] ? ` · ${names[s.competitorId]}` : ""} · {timeAgo(s.detectedAt)}
                    </div>
                    {s.summary && <div className="text-muted">{s.summary}</div>}
                    {s.url && (
                      <a href={s.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 break-all text-accent-soft hover:underline">
                        {s.url} <ExternalLink className="size-3 shrink-0" />
                      </a>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </div>
      </PageContainer>

      {editing && (
        <CompetitorDialog
          competitor={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await load();
          }}
        />
      )}
    </>
  );
}

function CompetitorDialog({
  competitor,
  onClose,
  onSaved,
}: {
  competitor: Competitor | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = React.useState<CompetitorInput>({
    name: competitor?.name ?? "",
    website: competitor?.website ?? "",
    notes: competitor?.notes ?? "",
    pages: competitor?.pages.length
      ? competitor.pages
      : [
          { kind: "pricing", url: "", render: "http" },
          { kind: "changelog", url: "", render: "http" },
        ],
  });
  const [error, setError] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  const setPage = (i: number, patch: Partial<CompetitorPage>) =>
    setForm((f) => ({ ...f, pages: f.pages.map((p, j) => (j === i ? { ...p, ...patch } : p)) }));

  async function save() {
    setSaving(true);
    setError("");
    const input = { ...form, pages: form.pages.filter((p) => p.url.trim()) };
    try {
      if (competitor) await updateCompetitor(competitor.id, input);
      else await createCompetitor(input);
      onSaved();
    } catch (e) {
      setError(apiErrorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} className="max-w-[640px]">
      <DialogHeader
        title={competitor ? `Edit ${competitor.name}` : "Add a competitor"}
        subtitle="Pages are fetched over plain HTTP (respecting robots.txt). Choose “browser” for pages that only render with JavaScript."
        onClose={onClose}
      />
      <div className="space-y-3 px-5 py-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block">
            <span className="mb-1 block text-[12px] font-medium text-muted">Name</span>
            <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Acme" />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12px] font-medium text-muted">Website</span>
            <Input value={form.website} onChange={(e) => setForm({ ...form, website: e.target.value })} placeholder="https://acme.com" />
          </label>
        </div>
        <div>
          <span className="mb-1 block text-[12px] font-medium text-muted">Pages to watch</span>
          <div className="space-y-2">
            {form.pages.map((p, i) => (
              <div key={i} className="flex gap-2">
                <select value={p.kind} onChange={(e) => setPage(i, { kind: e.target.value as CompetitorPage["kind"] })} className={SELECT} aria-label="Page kind">
                  {PAGE_KINDS.map((k) => (
                    <option key={k} value={k}>{k}</option>
                  ))}
                </select>
                <Input value={p.url} onChange={(e) => setPage(i, { url: e.target.value })} placeholder="https://acme.com/pricing" aria-label="Page URL" />
                <select value={p.render} onChange={(e) => setPage(i, { render: e.target.value as CompetitorPage["render"] })} className={SELECT} aria-label="Fetch with">
                  <option value="http">http</option>
                  <option value="browser">browser</option>
                </select>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setForm((f) => ({ ...f, pages: f.pages.filter((_, j) => j !== i) }))}
                  aria-label="Remove page"
                >
                  <Trash2 />
                </Button>
              </div>
            ))}
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="mt-2"
            onClick={() => setForm((f) => ({ ...f, pages: [...f.pages, { kind: "blog", url: "", render: "http" }] }))}
          >
            <Plus /> Add page
          </Button>
        </div>
        <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-muted">Notes (optional)</span>
          <Input value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} placeholder="Main rival in mid-market" />
        </label>
        {error && <p className="text-[12.5px] text-danger">{error}</p>}
      </div>
      <div className="flex justify-end gap-2 border-t border-border px-5 py-3">
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button variant="primary" onClick={save} disabled={saving || !form.name.trim()}>
          {saving ? "Saving…" : competitor ? "Save" : "Add competitor"}
        </Button>
      </div>
    </Dialog>
  );
}
