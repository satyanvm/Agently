import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { BrowserSessionView } from "@/components/browser-session";
import { Button } from "@/components/ui/button";
import { browserSession } from "@/lib/mock-data";

export default async function BrowserSessionPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  // All ids resolve to the one fully-modeled mock session.
  const session = { ...browserSession, id };

  return (
    <>
      <TopBar
        crumbs={[
          { label: "Browser Sessions", href: "/browser" },
          { label: session.agentName },
        ]}
        actions={
          <Link href="/runs/run-8842">
            <Button variant="secondary" size="sm">
              View run <ArrowUpRight className="size-3.5" />
            </Button>
          </Link>
        }
      />
      <PageContainer>
        <div className="mb-5">
          <h1 className="text-[20px] font-semibold tracking-tight text-fg">{session.pageTitle}</h1>
          <p className="mt-1 font-mono text-[12px] text-muted">{session.currentUrl}</p>
        </div>
        <BrowserSessionView session={session} />
      </PageContainer>
    </>
  );
}
