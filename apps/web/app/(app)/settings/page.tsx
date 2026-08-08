import Link from "next/link";
import { KeyRound, Server } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";

export default function SettingsPage() {
  return (
    <>
      <TopBar crumbs={[{ label: "Settings" }]} />
      <PageContainer>
        <PageTitle
          title="Settings"
          subtitle="Only persisted configuration is shown here."
        />

        <div className="grid max-w-[760px] gap-4 md:grid-cols-2">
          <Card className="p-5">
            <KeyRound className="size-5 text-accent-soft" />
            <h2 className="mt-4 text-[14px] font-semibold text-fg">Integration credentials</h2>
            <p className="mt-1 text-[12px] leading-relaxed text-muted">
              Credentials are stored through the workflow builder and attached to individual
              nodes. Secret values are never returned by the API.
            </p>
            <Link href="/workflows" className="mt-4 inline-block text-[12px] text-accent hover:underline">
              Open workflows
            </Link>
          </Card>

          <Card className="p-5">
            <Server className="size-5 text-running" />
            <h2 className="mt-4 text-[14px] font-semibold text-fg">Runtime configuration</h2>
            <p className="mt-1 text-[12px] leading-relaxed text-muted">
              Database, model, tracing, browser, delivery, and Temporal settings are read from
              the server environment. Missing required values prevent services from starting.
            </p>
          </Card>
        </div>
      </PageContainer>
    </>
  );
}
