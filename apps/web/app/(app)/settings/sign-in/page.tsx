"use client";

import * as React from "react";
import { CheckCircle2, Circle, Copy, ExternalLink } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { listOAuthApps, type PopularOAuthApp } from "@/lib/credentials";

/**
 * Operator checklist for one-click sign-in: register one OAuth app per
 * provider (one for all of Google, one for all of Microsoft), set its client
 * id/secret on the API, and every user gets "Sign in with X" on those nodes.
 */
export default function SignInAppsPage() {
  const [apps, setApps] = React.useState<PopularOAuthApp[] | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    listOAuthApps()
      .then(setApps)
      .catch((e) => setError(e instanceof Error ? e.message : "Could not load sign-in apps"));
  }, []);

  const configured = apps?.filter((a) => a.configured).length ?? 0;
  const redirectUrl = apps?.[0]?.redirectUrl;

  return (
    <>
      <TopBar crumbs={[{ label: "Settings", href: "/settings" }, { label: "Sign-in apps" }]} />
      <PageContainer>
        <PageTitle
          title="Sign-in apps"
          subtitle="Register an OAuth app once per provider and every user gets a “Sign in with …” button instead of pasting tokens."
        />

        {error && <p className="text-[13px] text-danger">{error}</p>}

        {redirectUrl && (
          <Card className="mb-4 max-w-[860px] p-4">
            <div className="text-[12px] font-medium text-fg">Redirect URL for every provider</div>
            <div className="mt-1.5 flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded bg-surface-2 px-2 py-1 font-mono text-[12px] text-fg">
                {redirectUrl}
              </code>
              <CopyButton text={redirectUrl} />
            </div>
            <p className="mt-2 text-[11px] leading-relaxed text-muted">
              Set the client id and secret as environment variables on the API (and the pieces worker
              and reasoner, which refresh expired tokens), then restart. {configured} of {apps?.length}{" "}
              configured.
            </p>
          </Card>
        )}

        <div className="max-w-[860px] divide-y divide-border rounded-lg border border-border bg-surface">
          {apps === null && !error && <p className="p-4 text-[12px] text-faint">Loading…</p>}
          {apps?.map((a) => (
            <div key={a.type} className="flex flex-wrap items-center gap-3 px-4 py-3">
              {a.configured ? (
                <CheckCircle2 className="size-4 shrink-0 text-success" />
              ) : (
                <Circle className="size-4 shrink-0 text-faint" />
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-[13px] font-medium text-fg">
                  {a.label}
                  {a.family && (
                    <Badge size="sm" variant="outline">
                      shared {a.family} app
                    </Badge>
                  )}
                </div>
                <div className="mt-0.5 font-mono text-[11px] text-muted break-all">
                  {a.family
                    ? `${a.familyClientIdEnv} / ${a.familyClientSecretEnv}`
                    : `${a.clientIdEnv} / ${a.clientSecretEnv}`}
                </div>
              </div>
              <Badge size="sm" variant={a.configured ? "success" : "neutral"}>
                {a.configured ? "Ready" : "Not set up"}
              </Badge>
              <a
                href={a.consoleUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-[12px] text-accent hover:underline"
              >
                Developer console <ExternalLink className="size-3" />
              </a>
            </div>
          ))}
        </div>
      </PageContainer>
    </>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = React.useState(false);
  return (
    <button
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1200);
        });
      }}
      className="inline-flex items-center gap-1 text-[12px] text-accent hover:text-accent-soft"
    >
      <Copy className="size-3.5" /> {copied ? "Copied" : "Copy"}
    </button>
  );
}
