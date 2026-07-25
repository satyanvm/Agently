"use client";

import * as React from "react";
import { ChevronDown, ChevronUp, Search } from "lucide-react";
import { SearchInput } from "@/components/ui/input";
import { NODE_CATALOG, NODE_KIND_META, type NodeSpec, type NodeKind } from "./node-catalog";
import { cn } from "@/lib/utils";

/**
 * The node palette. The catalog can hold thousands of nodes (built-ins +
 * generated integrations + Activepieces pieces), so the panel is search-driven
 * and renders capped, expandable groups instead of the whole list: built-ins
 * group by kind (Triggers/Agents/…), integrations group by cluster.
 */

const KIND_ORDER: NodeKind[] = ["trigger", "agent", "tool", "logic", "output"];
/** Nodes shown per group before "Show all" (keeps the DOM small at scale). */
const COLLAPSED_LIMIT = 8;
/** Matches shown per group while searching, until the group is expanded. */
const SEARCH_LIMIT = 20;

interface PaletteGroup {
  id: string;
  label: string;
  tone: string;
  nodes: NodeSpec[];
}

/** Static grouping — the catalog doesn't change at runtime. */
function buildGroups(): PaletteGroup[] {
  const builtins = new Map<NodeKind, NodeSpec[]>();
  const clusters = new Map<string, { label: string; nodes: NodeSpec[] }>();

  for (const node of NODE_CATALOG) {
    if (node.cluster) {
      const group = clusters.get(node.cluster) ?? {
        label: node.clusterLabel ?? node.cluster,
        nodes: [],
      };
      group.nodes.push(node);
      clusters.set(node.cluster, group);
    } else {
      const list = builtins.get(node.kind) ?? [];
      list.push(node);
      builtins.set(node.kind, list);
    }
  }

  const kindGroups: PaletteGroup[] = KIND_ORDER.flatMap((kind) => {
    const nodes = builtins.get(kind) ?? [];
    if (nodes.length === 0) return [];
    const meta = NODE_KIND_META[kind];
    return [{ id: `kind:${kind}`, label: meta.label, tone: meta.tone, nodes }];
  });

  const clusterGroups: PaletteGroup[] = [...clusters.entries()]
    .map(([id, g]) => ({ id: `cluster:${id}`, label: g.label, tone: "text-muted", nodes: g.nodes }))
    .sort((a, b) => a.label.localeCompare(b.label));

  return [...kindGroups, ...clusterGroups];
}

const ALL_GROUPS = buildGroups();

function matches(node: NodeSpec, query: string): boolean {
  return (
    node.label.toLowerCase().includes(query) ||
    node.description.toLowerCase().includes(query) ||
    node.id.toLowerCase().includes(query) ||
    (node.clusterLabel?.toLowerCase().includes(query) ?? false)
  );
}

export function NodeCatalog() {
  const [search, setSearch] = React.useState("");
  const [expanded, setExpanded] = React.useState<Set<string>>(new Set());

  const onDragStart = (event: React.DragEvent, nodeSpec: NodeSpec) => {
    event.dataTransfer.setData("application/reactflow", nodeSpec.id);
    event.dataTransfer.effectAllowed = "move";
  };

  const groups = React.useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return ALL_GROUPS;
    return ALL_GROUPS.flatMap((g) => {
      const nodes = g.nodes.filter((n) => matches(n, query));
      return nodes.length > 0 ? [{ ...g, nodes }] : [];
    });
  }, [search]);

  const toggleExpanded = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const searching = search.trim() !== "";
  const limit = searching ? SEARCH_LIMIT : COLLAPSED_LIMIT;

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
        {groups.length === 0 && (
          <div className="text-[12px] text-faint text-center py-10">
            No nodes match &ldquo;{search}&rdquo;
          </div>
        )}
        {groups.map((group) => {
          const isExpanded = expanded.has(group.id);
          const visible = isExpanded ? group.nodes : group.nodes.slice(0, limit);
          const hiddenCount = group.nodes.length - visible.length;

          return (
            <div key={group.id}>
              <h3
                className={cn(
                  "flex items-baseline gap-1.5 text-[11px] font-semibold uppercase tracking-wider mb-2 px-1",
                  group.tone,
                )}
              >
                {group.label}
                <span className="text-faint font-normal">{group.nodes.length}</span>
              </h3>
              <div className="space-y-1">
                {visible.map((node) => (
                  <NodeCatalogItem key={node.id} node={node} onDragStart={onDragStart} />
                ))}
              </div>
              {(hiddenCount > 0 || (isExpanded && group.nodes.length > limit)) && (
                <button
                  onClick={() => toggleExpanded(group.id)}
                  className="mt-1 flex w-full items-center justify-center gap-1 rounded-md px-2 py-1.5 text-[11px] text-muted transition-colors hover:bg-surface-2 hover:text-fg"
                >
                  {hiddenCount > 0 ? (
                    <>
                      <ChevronDown className="size-3" />
                      Show all {group.nodes.length}
                    </>
                  ) : (
                    <>
                      <ChevronUp className="size-3" />
                      Show less
                    </>
                  )}
                </button>
              )}
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
