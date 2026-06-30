"use client";

import * as React from "react";
import { Search } from "lucide-react";
import { SearchInput } from "@/components/ui/input";
import { NODE_CATALOG, NODE_KIND_META, type NodeSpec, type NodeKind } from "./node-catalog";
import { cn } from "@/lib/utils";

export function NodeCatalog() {
  const [search, setSearch] = React.useState("");

  const onDragStart = (event: React.DragEvent, nodeSpec: NodeSpec) => {
    event.dataTransfer.setData("application/reactflow", nodeSpec.id);
    event.dataTransfer.effectAllowed = "move";
  };

  const filteredNodes = React.useMemo(() => {
    if (!search) return NODE_CATALOG;
    const query = search.toLowerCase();
    return NODE_CATALOG.filter(
      (node) =>
        node.label.toLowerCase().includes(query) ||
        node.description.toLowerCase().includes(query) ||
        node.kind.toLowerCase().includes(query)
    );
  }, [search]);

  const groupedNodes = React.useMemo(() => {
    const groups: Record<NodeKind, NodeSpec[]> = {
      trigger: [],
      agent: [],
      tool: [],
      logic: [],
      output: [],
    };
    filteredNodes.forEach((node) => {
      groups[node.kind].push(node);
    });
    return groups;
  }, [filteredNodes]);

  return (
    <div className="w-[280px] border-r border-border bg-surface flex flex-col h-full">
      {/* Header */}
      <div className="px-4 py-4 border-b border-border">
        <h2 className="text-[13px] font-semibold text-fg mb-3">Node Catalog</h2>
        <SearchInput
          icon={<Search className="size-4" />}
          placeholder="Search nodes..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="!h-8 text-[13px]"
        />
      </div>

      {/* Node groups */}
      <div className="flex-1 overflow-y-auto p-3 space-y-4">
        {(Object.keys(groupedNodes) as NodeKind[]).map((kind) => {
          const nodes = groupedNodes[kind];
          if (nodes.length === 0) return null;

          const meta = NODE_KIND_META[kind];
          return (
            <div key={kind}>
              <h3 className={cn("text-[11px] font-semibold uppercase tracking-wider mb-2 px-1", meta.tone)}>
                {meta.label}
              </h3>
              <div className="space-y-1">
                {nodes.map((node) => (
                  <NodeCatalogItem
                    key={node.id}
                    node={node}
                    onDragStart={onDragStart}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>

      {/* Footer hint */}
      <div className="px-4 py-3 border-t border-border text-[11px] text-faint">
        Drag nodes onto the canvas to build your workflow
      </div>
    </div>
  );
}

function NodeCatalogItem({
  node,
  onDragStart,
}: {
  node: NodeSpec;
  onDragStart: (event: React.DragEvent, node: NodeSpec) => void;
}) {
  const Icon = node.icon;

  return (
    <div
      draggable
      onDragStart={(e) => onDragStart(e, node)}
      className={cn(
        "flex items-start gap-2.5 p-2.5 rounded-md border border-border bg-surface",
        "cursor-grab active:cursor-grabbing",
        "transition-all hover:border-border-strong hover:bg-surface-2 hover:shadow-sm"
      )}
    >
      <div className={cn("flex h-7 w-7 shrink-0 items-center justify-center rounded-md", node.toneBg)}>
        <Icon className={cn("size-4", node.tone)} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-[13px] font-medium text-fg leading-tight">{node.label}</div>
        <div className="text-[11px] text-muted leading-snug mt-0.5">{node.description}</div>
      </div>
    </div>
  );
}
