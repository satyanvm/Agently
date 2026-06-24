"use client";

import * as React from "react";
import { Building2, CreditCard, Users, KeyRound, Server, Copy, Check, Mail, Loader2 } from "lucide-react";
import {
  fetchIntegrations,
  disconnectGoogle,
  connectGoogleURL,
  type IntegrationStatus,
} from "@/lib/api";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Avatar } from "@/components/ui/avatar";
import { Switch } from "@/components/ui/switch";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";

const SECTIONS = [
  { id: "general", label: "General", icon: Building2 },
  { id: "billing", label: "Billing & usage", icon: CreditCard },
  { id: "members", label: "Members", icon: Users },
  { id: "integrations", label: "Integrations", icon: Mail },
  { id: "api", label: "API keys", icon: KeyRound },
  { id: "compute", label: "Compute", icon: Server },
] as const;

type Section = (typeof SECTIONS)[number]["id"];

export default function SettingsPage() {
  const [section, setSection] = React.useState<Section>("general");

  return (
    <>
      <TopBar crumbs={[{ label: "Settings" }]} />
      <PageContainer>
        <PageTitle title="Settings" subtitle="Manage your workspace, billing, and access." />

        <div className="grid gap-6 lg:grid-cols-[200px_1fr]">
          {/* Section nav */}
          <nav className="space-y-0.5">
            {SECTIONS.map((s) => {
              const Icon = s.icon;
              const active = section === s.id;
              return (
                <button
                  key={s.id}
                  onClick={() => setSection(s.id)}
                  className={cn(
                    "flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-[13px] font-medium transition-colors",
                    active ? "bg-surface-2 text-fg" : "text-muted hover:bg-surface-2/50 hover:text-fg",
                  )}
                >
                  <Icon className={cn("size-4", active ? "text-accent-soft" : "text-faint")} />
                  {s.label}
                </button>
              );
            })}
          </nav>

          <div className="max-w-[680px] space-y-5">
            {section === "general" && <General />}
            {section === "billing" && <Billing />}
            {section === "members" && <Members />}
            {section === "integrations" && <Integrations />}
            {section === "api" && <ApiKeys />}
            {section === "compute" && <Compute />}
          </div>
        </div>
      </PageContainer>
    </>
  );
}

function Row({ title, desc, children }: { title: string; desc?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-border px-5 py-4 last:border-0">
      <div>
        <div className="text-[13px] font-medium text-fg">{title}</div>
        {desc && <div className="mt-0.5 text-[12px] text-faint">{desc}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

function General() {
  return (
    <Card>
      <div className="border-b border-border px-5 py-4">
        <h3 className="text-[14px] font-semibold text-fg">Workspace</h3>
      </div>
      <Row title="Workspace name">
        <Input defaultValue="Northwind Labs" className="w-[240px]" />
      </Row>
      <Row title="Workspace URL" desc="agently.dev/northwind">
        <Input defaultValue="northwind" className="w-[240px]" />
      </Row>
      <Row title="Default region" desc="Where new runs are scheduled">
        <select className="h-9 rounded-md border border-border bg-surface px-3 text-sm text-fg focus:outline-none">
          <option>us-east-1</option>
          <option>us-west-2</option>
          <option>eu-central-1</option>
          <option>ap-southeast-1</option>
        </select>
      </Row>
      <Row title="Run history retention" desc="Logs & artifacts kept per run">
        <select className="h-9 rounded-md border border-border bg-surface px-3 text-sm text-fg focus:outline-none">
          <option>30 days</option>
          <option>90 days</option>
          <option>1 year</option>
        </select>
      </Row>
      <div className="flex justify-end px-5 py-4">
        <Button variant="primary" size="sm">Save changes</Button>
      </div>
    </Card>
  );
}

function Billing() {
  return (
    <>
      <Card className="px-5 py-5">
        <div className="flex items-center justify-between">
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-[14px] font-semibold text-fg">Pro plan</h3>
              <Badge variant="accent" size="sm">current</Badge>
            </div>
            <p className="mt-1 text-[12px] text-muted">$0.04 / agent-hour · billed monthly</p>
          </div>
          <Button variant="secondary" size="sm">Manage plan</Button>
        </div>
        <div className="mt-5 space-y-4">
          <Usage label="Agent-hours" used={648} total={1000} unit="h" />
          <Usage label="Browser minutes" used={2840} total={5000} unit="m" />
          <Usage label="Storage" used={42} total={100} unit="GB" />
        </div>
      </Card>
      <Card className="px-5 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-12 items-center justify-center rounded-md bg-surface-2 text-[11px] font-semibold ring-1 ring-border">VISA</div>
            <div>
              <div className="text-[13px] text-fg">•••• 4242</div>
              <div className="text-[11px] text-faint">expires 08/28</div>
            </div>
          </div>
          <Button variant="ghost" size="sm">Update</Button>
        </div>
      </Card>
    </>
  );
}

function Usage({ label, used, total, unit }: { label: string; used: number; total: number; unit: string }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between text-[12px]">
        <span className="text-muted">{label}</span>
        <span className="tabular-nums text-faint">
          {used.toLocaleString()} / {total.toLocaleString()} {unit}
        </span>
      </div>
      <Progress value={used / total} tone={used / total > 0.85 ? "warn" : "accent"} />
    </div>
  );
}

function Members() {
  const people = [
    { name: "Maya Chen", initials: "MC", email: "maya@northwind.dev", role: "Owner" },
    { name: "Dev Patel", initials: "DP", email: "dev@northwind.dev", role: "Admin" },
    { name: "Lena Ortiz", initials: "LO", email: "lena@northwind.dev", role: "Member" },
    { name: "Sam Idowu", initials: "SI", email: "sam@northwind.dev", role: "Member" },
  ];
  return (
    <Card>
      <div className="flex items-center justify-between border-b border-border px-5 py-4">
        <h3 className="text-[14px] font-semibold text-fg">Members</h3>
        <Button variant="primary" size="sm">Invite</Button>
      </div>
      {people.map((p) => (
        <div key={p.email} className="flex items-center gap-3 border-b border-border px-5 py-3 last:border-0">
          <Avatar initials={p.initials} size="md" />
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-medium text-fg">{p.name}</div>
            <div className="text-[12px] text-faint">{p.email}</div>
          </div>
          <Badge variant={p.role === "Owner" ? "accent" : "neutral"} size="sm">{p.role}</Badge>
        </div>
      ))}
    </Card>
  );
}

function Integrations() {
  const [items, setItems] = React.useState<IntegrationStatus[] | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [banner, setBanner] = React.useState<{ kind: "ok" | "err"; text: string } | null>(null);

  const load = React.useCallback(async () => {
    try {
      setItems(await fetchIntegrations());
    } catch {
      setItems([]);
    }
  }, []);

  React.useEffect(() => {
    load();
    // Reflect the OAuth callback result (?google=connected|error) as a banner.
    const g = new URLSearchParams(window.location.search).get("google");
    if (g === "connected") setBanner({ kind: "ok", text: "Gmail connected — runs will now email from your Google account." });
    else if (g === "error") setBanner({ kind: "err", text: "Couldn't connect Gmail. Please try again." });
  }, [load]);

  const google = items?.find((i) => i.provider === "google");

  const onDisconnect = async () => {
    setBusy(true);
    await disconnectGoogle();
    await load();
    setBusy(false);
  };

  return (
    <Card>
      <div className="border-b border-border px-5 py-4">
        <h3 className="text-[14px] font-semibold text-fg">Email & integrations</h3>
        <p className="mt-0.5 text-[12px] text-faint">
          Connect an account so your digests are sent from a real address.
        </p>
      </div>

      {banner && (
        <div
          className={cn(
            "mx-5 mt-4 rounded-md border px-3 py-2 text-[12px]",
            banner.kind === "ok"
              ? "border-success/30 bg-success/10 text-success"
              : "border-danger/30 bg-danger-bg text-danger",
          )}
        >
          {banner.text}
        </div>
      )}

      <Row
        title="Gmail (send as you)"
        desc={
          google == null
            ? "Loading…"
            : !google.configured
              ? "Set GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET to enable OAuth."
              : google.connected
                ? `Connected${google.accountEmail ? " · " + google.accountEmail : ""}`
                : "Send digests from your own Gmail via OAuth2."
        }
      >
        {google?.connected ? (
          <div className="flex items-center gap-2">
            <Badge variant="accent" size="sm">connected</Badge>
            <Button variant="secondary" size="sm" onClick={onDisconnect} disabled={busy}>
              {busy ? <Loader2 className="size-3.5 animate-spin" /> : null} Disconnect
            </Button>
          </div>
        ) : (
          <Button
            variant="primary"
            size="sm"
            disabled={google != null && !google.configured}
            onClick={() => {
              window.location.href = connectGoogleURL;
            }}
          >
            <Mail className="size-4" /> Connect Gmail
          </Button>
        )}
      </Row>

      <div className="px-5 py-4 text-[12px] text-faint">
        No account connected? Runs fall back to a transactional provider (Resend,
        from the app’s own domain) or SMTP — whichever is configured on the worker.
      </div>
    </Card>
  );
}

function ApiKeys() {
  const [copied, setCopied] = React.useState(false);
  const key = "agt_live_sk_9f2a••••••••••••••••4c1d";
  return (
    <Card>
      <div className="flex items-center justify-between border-b border-border px-5 py-4">
        <h3 className="text-[14px] font-semibold text-fg">API keys</h3>
        <Button variant="primary" size="sm">Create key</Button>
      </div>
      <div className="px-5 py-4">
        <div className="flex items-center gap-2 rounded-md border border-border bg-inset px-3 py-2.5">
          <KeyRound className="size-4 text-faint" />
          <code className="flex-1 font-mono text-[12px] text-muted">{key}</code>
          <button
            onClick={() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1400);
            }}
            className="flex items-center gap-1 rounded px-2 py-1 text-[12px] text-faint hover:bg-surface-3 hover:text-fg"
          >
            {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
        <p className="mt-2 text-[11px] text-faint">
          Created Mar 2, 2026 · last used 4 minutes ago · full access
        </p>
      </div>
    </Card>
  );
}

function Compute() {
  return (
    <Card>
      <div className="border-b border-border px-5 py-4">
        <h3 className="text-[14px] font-semibold text-fg">Sandbox defaults</h3>
      </div>
      <Row title="Default sandbox size" desc="vCPU / memory per run">
        <select className="h-9 rounded-md border border-border bg-surface px-3 text-sm text-fg focus:outline-none">
          <option>4 vCPU / 8 GiB</option>
          <option>2 vCPU / 4 GiB</option>
          <option>8 vCPU / 16 GiB</option>
        </select>
      </Row>
      <Row title="Max concurrent runs" desc="Across the workspace">
        <Input defaultValue="12" className="w-[100px] text-center" />
      </Row>
      <Row title="Auto-scale sandboxes" desc="Provision on demand under load">
        <DefaultSwitch on />
      </Row>
      <Row title="Persistent workspace cache" desc="Reuse dependency cache between runs">
        <DefaultSwitch on />
      </Row>
    </Card>
  );
}

function DefaultSwitch({ on }: { on?: boolean }) {
  const [v, setV] = React.useState(!!on);
  return <Switch checked={v} onChange={setV} />;
}
