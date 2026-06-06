"use client";

import * as React from "react";
import { ArrowRight, MessageSquare, Network, Maximize2 } from "lucide-react";
import type { AgentNode, AgentMessage } from "@/lib/types";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { StatusBadge, StatusDot, statusMeta } from "@/components/ui/status";
import { AgentGraph, GraphLegend } from "@/components/agent-graph";
import { AgentGlyph, roleMeta } from "@/components/agent-glyph";
import { Segmented } from "@/components/ui/segmented";
import { cn, formatClock } from "@/lib/utils";

export function AgentVisualization({
  agents,
  messages,
}: {
  agents: AgentNode[];
  messages: AgentMessage[];
}) {
  const [selectedId, setSelectedId] = React.useState<string | null>(null);
  const [tab, setTab] = React.useState<"comms" | "deps">("comms");
  const nameOf = (id: string) => agents.find((a) => a.id === id)?.name ?? id;
  const roleOf = (id: string) => agents.find((a) => a.id === id)?.role;

  const visibleMessages = selectedId
    ? messages.filter((m) => m.from === selectedId || m.to === selectedId)
    : messages;

  return (
    <div className="grid gap-5 lg:grid-cols-[1.7fr_1fr]">
      {/* Graph canvas */}
      <Card className="bg-surface/60">
        <div className="flex items-center justify-between border-b border-border px-5 py-3">
          <div className="flex items-center gap-2">
            <Network className="size-4 text-accent-soft" />
            <h3 className="text-[13px] font-semibold text-fg">Multi-agent topology</h3>
            <Badge variant="neutral" size="sm">
              {agents.length} agents
            </Badge>
          </div>
          <button className="flex items-center gap-1.5 text-[12px] text-muted hover:text-fg">
            <Maximize2 className="size-3.5" /> Fullscreen
          </button>
        </div>
        <div className="bg-dots p-4">
          <AgentGraph agents={agents} selectedId={selectedId} onSelect={(id) => setSelectedId((p) => (p === id ? null : id))} />
        </div>
        <div className="flex items-center justify-between border-t border-border px-5 py-2.5">
          <GraphLegend />
          {selectedId ? (
            <button
              onClick={() => setSelectedId(null)}
              className="text-[11px] text-accent-soft hover:underline"
            >
              Clear selection
            </button>
          ) : (
            <span className="text-[11px] text-faint">Click an agent to focus its messages</span>
          )}
        </div>
      </Card>

      {/* Side: communication + dependencies */}
      <Card className="flex flex-col">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <Segmented<"comms" | "deps">
            size="sm"
            value={tab}
            onChange={setTab}
            options={[
              { value: "comms", label: "Communication", count: visibleMessages.length },
              { value: "deps", label: "Dependencies" },
            ]}
          />
        </div>

        {tab === "comms" ? (
          <div className="max-h-[520px] flex-1 overflow-y-auto px-4 py-3">
            <ol className="space-y-2.5">
              {visibleMessages.map((m) => (
                <li
                  key={m.id}
                  className="rounded-lg border border-border bg-surface-2/40 px-3 py-2.5"
                >
                  <div className="flex items-center gap-1.5 text-[12px]">
                    <span className="inline-flex items-center gap-1.5 font-medium text-fg">
                      {roleOf(m.from) && <AgentGlyph role={roleOf(m.from)!} size="sm" className="!h-5 !w-5 [&_svg]:size-3" />}
                      {nameOf(m.from)}
                    </span>
                    <ArrowRight className="size-3 text-accent-soft" />
                    <span className="font-medium text-fg">{nameOf(m.to)}</span>
                    <span className="ml-auto font-mono text-[10px] text-ghost">{formatClock(m.at)}</span>
                  </div>
                  <div className="mt-1.5 flex items-center gap-1.5 pl-0.5">
                    <MessageSquare className="size-3 text-faint" />
                    <span className="text-[12px] text-muted">{m.label}</span>
                  </div>
                </li>
              ))}
              {visibleMessages.length === 0 && (
                <li className="py-10 text-center text-[12px] text-faint">No messages yet.</li>
              )}
            </ol>
          </div>
        ) : (
          <div className="flex-1 px-3 py-3">
            {agents.map((a) => {
              const m = statusMeta(a.status);
              const rm = roleMeta(a.role);
              return (
                <div key={a.id} className="rounded-md px-2 py-2">
                  <div className="flex items-center gap-2">
                    <AgentGlyph role={a.role} size="sm" />
                    <span className="text-[13px] font-medium text-fg">{a.name}</span>
                    <span className={cn("ml-auto text-[11px] font-medium", m.fg)}>{m.label}</span>
                    <StatusDot status={a.status} />
                  </div>
                  <div className="mt-1.5 flex flex-wrap items-center gap-1.5 pl-8">
                    {a.dependsOn.length === 0 ? (
                      <Badge variant="accent" size="sm">entry point</Badge>
                    ) : (
                      a.dependsOn.map((d) => (
                        <Badge key={d} variant="outline" size="sm">
                          ← {nameOf(d)}
                        </Badge>
                      ))
                    )}
                    <span className={cn("ml-1 text-[10px]", rm.tone)}>{rm.label}</span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>
    </div>
  );
}
