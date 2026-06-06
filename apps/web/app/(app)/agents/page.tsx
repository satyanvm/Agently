import { Wrench } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { StatusBadge } from "@/components/ui/status";
import { AgentGlyph, roleMeta } from "@/components/agent-glyph";
import { agentLibrary } from "@/lib/mock-data";
import { formatCost, formatCompact } from "@/lib/utils";

export default function AgentsPage() {
  return (
    <>
      <TopBar
        crumbs={[{ label: "Agents" }]}
        actions={
          <Button variant="primary" size="sm">
            New agent
          </Button>
        }
      />
      <PageContainer>
        <PageTitle
          title="Agents"
          subtitle="Reusable agent definitions — the building blocks of your workflows."
        />

        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {agentLibrary.map((a) => {
            const m = roleMeta(a.role);
            return (
              <Card key={a.id} hover className="flex flex-col p-4">
                <div className="flex items-start gap-3">
                  <AgentGlyph role={a.role} size="lg" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between gap-2">
                      <h3 className="truncate text-[14px] font-semibold tracking-tight text-fg">
                        {a.name}
                      </h3>
                      <StatusBadge status={a.status} size="sm" />
                    </div>
                    <span className={`text-[11px] font-medium ${m.tone}`}>{m.label}</span>
                  </div>
                </div>

                <p className="mt-3 text-[12px] leading-relaxed text-muted">{a.description}</p>

                <div className="mt-3 flex flex-wrap items-center gap-1.5">
                  {a.tools.map((t) => (
                    <Badge key={t} variant="neutral" size="sm" className="font-mono">
                      {t}
                    </Badge>
                  ))}
                </div>

                <div className="mt-4 grid grid-cols-3 gap-2 border-t border-border pt-3">
                  <Metric label="Model" value={a.model.replace("claude-", "").replace(/-/g, " ")} mono />
                  <Metric label="Runs · 7d" value={formatCompact(a.runs7d)} />
                  <Metric label="Avg cost" value={formatCost(a.avgCostUsd)} />
                </div>

                <div className="mt-3 flex items-center gap-1.5 text-[11px] text-faint">
                  <Wrench className="size-3" />
                  Used in {a.workflows.length} workflow{a.workflows.length > 1 ? "s" : ""}
                </div>
              </Card>
            );
          })}
        </div>
      </PageContainer>
    </>
  );
}

function Metric({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div
        className={`truncate text-[12px] font-semibold capitalize text-fg ${mono ? "font-mono text-[11px]" : "tabular-nums"}`}
      >
        {value}
      </div>
      <div className="text-[10px] text-faint">{label}</div>
    </div>
  );
}
