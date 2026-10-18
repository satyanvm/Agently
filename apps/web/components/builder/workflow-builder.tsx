"use client";

import * as React from "react";
import {
  ReactFlow,
  Node,
  Edge,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  addEdge,
  Connection,
  ConnectionMode,
  Panel,
  BackgroundVariant,
  type OnConnect,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import Link from "next/link";
import { Save, Play, ShieldAlert, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { NodeCatalog } from "./node-catalog-panel";
import { NodeInspector } from "./node-inspector";
import { WorkflowNode } from "./workflow-node";
import { CredentialsProvider, useCredentials } from "./credentials-context";
import { CREDENTIAL_ID_KEY, defaultConfig, findNodeSpec } from "./node-catalog";
import { NodeLoginModal, RunLoginGate, nodeNeedsLogin } from "./login-prompt";
import { getWorkflowGraph, saveWorkflowGraph } from "@/lib/api";
import { PINNED_KEY, fetchNodeData, runAndWait, type NodeSamples, type RunState } from "@/lib/builder-debug";
import { RunDataContext } from "./run-data-context";
import { WorkflowSettingsDialog } from "./workflow-settings-dialog";
import { maxNumericNodeId } from "@/lib/builder-graph";
import { cn } from "@/lib/utils";

const nodeTypes = {
  workflow: WorkflowNode,
};

interface WorkflowBuilderProps {
  workflowSlug: string;
}

export interface NodeData extends Record<string, unknown> {
  label: string;
  typeId: string;
  config: Record<string, unknown>;
}

export type WorkflowNodeType = Node<NodeData>;

let nodeIdCounter = 0;

export function WorkflowBuilder(props: WorkflowBuilderProps) {
  // Credentials are shared by the canvas (needs-creds badges), the inspector
  // (credential picker) and the login prompts; one provider keeps them in sync.
  return (
    <CredentialsProvider>
      <WorkflowBuilderInner {...props} />
    </CredentialsProvider>
  );
}

function WorkflowBuilderInner({ workflowSlug }: WorkflowBuilderProps) {
  const { credentials, refresh: refreshCredentials } = useCredentials();
  // Nodes dropped on the canvas that need a login, prompted one at a time.
  const [loginQueue, setLoginQueue] = React.useState<string[]>([]);
  const [loginNodeId, setLoginNodeId] = React.useState<string | null>(null);
  const [runGateOpen, setRunGateOpen] = React.useState(false);
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowNodeType>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [selectedNodeId, setSelectedNodeId] = React.useState<string | null>(null);
  const [isSaving, setIsSaving] = React.useState(false);
  const [isRunning, setIsRunning] = React.useState(false);
  // Latest output/error per node (Data tab, canvas status) and the test run in flight.
  const [samples, setSamples] = React.useState<NodeSamples | null>(null);
  const [stepRunning, setStepRunning] = React.useState<string | null>(null);
  const [lastRun, setLastRun] = React.useState<{ id: string; number: number; status: RunState; step: string; target?: string; error?: string | null } | null>(null);
  const [settingsOpen, setSettingsOpen] = React.useState(false);
  const reactFlowWrapper = React.useRef<HTMLDivElement>(null);
  const [reactFlowInstance, setReactFlowInstance] = React.useState<any>(null);

  // Load workflow graph on mount
  React.useEffect(() => {
    loadWorkflow();
    refreshSamples();
  }, [workflowSlug]);

  const refreshSamples = React.useCallback(() => {
    fetchNodeData(workflowSlug)
      .then(setSamples)
      .catch(() => setSamples({}));
  }, [workflowSlug]);

  const loadWorkflow = async () => {
    try {
      const { nodes: loadedNodes, edges: loadedEdges } =
        await getWorkflowGraph(workflowSlug);
      setNodes(loadedNodes);
      setEdges(loadedEdges);
      // Resume the id counter past any loaded numeric ids to avoid collisions.
      nodeIdCounter = maxNumericNodeId(loadedNodes) + 1;
    } catch (err) {
      console.error("Failed to load workflow:", err);
    }
  };

  const saveWorkflow = async (): Promise<boolean> => {
    setIsSaving(true);
    try {
      await saveWorkflowGraph(workflowSlug, { nodes, edges });
      return true;
    } catch (err) {
      console.error("Failed to save workflow:", err);
      return false;
    } finally {
      setIsSaving(false);
    }
  };

  const missingLogins = React.useMemo(
    () => nodes.filter((n) => nodeNeedsLogin(n, credentials)),
    [nodes, credentials],
  );

  const handleTestRun = () => {
    if (missingLogins.length > 0) {
      setRunGateOpen(true);
      return;
    }
    void launchTestRun();
  };

  /**
   * Save, launch a test run and follow it here instead of leaving the editor
   * (n8n-style). `targetNode` = "Run step": only that node and its ancestors
   * run, ancestors reuse their last outputs. Test runs always honour pins.
   */
  const startTestRun = async (targetNode?: string) => {
    setRunGateOpen(false);
    setIsRunning(true);
    setStepRunning(targetNode ?? null);
    try {
      const saved = await saveWorkflow();
      if (!saved) return;
      const done = await runAndWait(workflowSlug, { targetNode, usePins: true }, (r) =>
        setLastRun({ id: r.id, number: r.number, status: r.status, step: r.currentStep, target: targetNode, error: r.error }),
      );
      setLastRun({ id: done.id, number: done.number, status: done.status, step: done.currentStep, target: targetNode, error: done.error });
    } catch (err) {
      setLastRun({ id: "", number: 0, status: "failed", step: "", target: targetNode, error: err instanceof Error ? err.message : String(err) });
    } finally {
      setIsRunning(false);
      setStepRunning(null);
      refreshSamples();
    }
  };

  const launchTestRun = () => startTestRun();

  const onConnect: OnConnect = React.useCallback(
    (connection: Connection) => {
      setEdges((eds) => addEdge(connection, eds));
    },
    [setEdges]
  );

  const onDragOver = React.useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
  }, []);

  const onDrop = React.useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();

      const typeId = event.dataTransfer.getData("application/reactflow");
      if (!typeId || !reactFlowWrapper.current || !reactFlowInstance) return;

      const spec = findNodeSpec(typeId);
      if (!spec) return;

      const bounds = reactFlowWrapper.current.getBoundingClientRect();
      const position = reactFlowInstance.screenToFlowPosition({
        x: event.clientX - bounds.left,
        y: event.clientY - bounds.top,
      });

      const newNode: Node<NodeData> = {
        id: `${nodeIdCounter++}`,
        type: "workflow",
        position,
        data: {
          label: spec.label,
          typeId: spec.id,
          config: defaultConfig(spec.id),
        },
      };

      setNodes((nds) => nds.concat(newNode));
      if (spec.credentialType) {
        setSelectedNodeId(newNode.id);
        setLoginQueue((q) => [...q, newNode.id]);
      }
    },
    [reactFlowInstance, setNodes]
  );

  /** Attach a credential to every node of its type that still lacks one. */
  const attachCredential = React.useCallback(
    (credentialType: string, credentialId: string, onlyNodeId?: string) => {
      setNodes((nds) =>
        nds.map((n) => {
          if (onlyNodeId ? n.id !== onlyNodeId : findNodeSpec(n.data.typeId)?.credentialType !== credentialType) {
            return n;
          }
          const cur = n.data.config[CREDENTIAL_ID_KEY];
          if (!onlyNodeId && typeof cur === "string" && cur !== "" && credentials?.some((c) => c.id === cur)) {
            return n;
          }
          return { ...n, data: { ...n.data, config: { ...n.data.config, [CREDENTIAL_ID_KEY]: credentialId } } };
        }),
      );
    },
    [setNodes, credentials],
  );

  // Work the login queue: a freshly dropped node reuses the workspace's
  // account for that app when one exists (most recently updated wins — the
  // inspector can switch it), otherwise the sign-in modal opens right away.
  React.useEffect(() => {
    if (loginNodeId || loginQueue.length === 0 || credentials === null) return;
    const [head, ...rest] = loginQueue;
    setLoginQueue(rest);
    const node = nodes.find((n) => n.id === head);
    const type = node && findNodeSpec(node.data.typeId)?.credentialType;
    if (!node || !type || !nodeNeedsLogin(node, credentials)) return;
    const [latest] = credentials
      .filter((c) => c.type === type)
      .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    if (latest) attachCredential(type, latest.id, node.id);
    else setLoginNodeId(node.id);
  }, [loginQueue, loginNodeId, credentials, nodes, attachCredential]);

  const loginNode = nodes.find((n) => n.id === loginNodeId);

  const onNodeClick = React.useCallback((_: React.MouseEvent, node: Node) => {
    setSelectedNodeId(node.id);
  }, []);

  const onPaneClick = React.useCallback(() => {
    setSelectedNodeId(null);
  }, []);

  const updateNodeConfig = React.useCallback(
    (nodeId: string, config: Record<string, unknown>) => {
      setNodes((nds) =>
        nds.map((node) =>
          node.id === nodeId
            ? { ...node, data: { ...node.data, config } }
            : node
        )
      );
    },
    [setNodes]
  );

  const deleteSelectedNode = React.useCallback(() => {
    if (!selectedNodeId) return;
    setNodes((nds) => nds.filter((n) => n.id !== selectedNodeId));
    setEdges((eds) =>
      eds.filter((e) => e.source !== selectedNodeId && e.target !== selectedNodeId)
    );
    setSelectedNodeId(null);
  }, [selectedNodeId, setNodes, setEdges]);

  const selectedNode = nodes.find((n) => n.id === selectedNodeId);
  const selectedDeps = React.useMemo(
    () =>
      edges
        .filter((e) => e.target === selectedNodeId)
        .map((e) => {
          const src = nodes.find((n) => n.id === e.source);
          return {
            key: e.source,
            label: src?.data.label ?? e.source,
            pinned: src?.data.config[PINNED_KEY] as Record<string, unknown> | undefined,
          };
        }),
    [edges, nodes, selectedNodeId],
  );

  return (
    <RunDataContext.Provider value={samples}>
    <div className="flex h-full w-full">
      {/* Left sidebar - Node Catalog */}
      <NodeCatalog />

      {/* Main canvas */}
      <div className="flex-1 relative" ref={reactFlowWrapper}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onInit={setReactFlowInstance}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
          nodeTypes={nodeTypes}
          connectionMode={ConnectionMode.Loose}
          fitView
          className="bg-bg"
          deleteKeyCode="Delete"
        >
          <Background
            variant={BackgroundVariant.Dots}
            gap={16}
            size={1}
            color="rgba(58, 40, 22, 0.12)"
          />
          <Controls
            className="!border-border !bg-surface !shadow-card [&_button]:!border-border [&_button]:!bg-surface-2 [&_button:hover]:!bg-surface-3"
            showInteractive={false}
          />

          {/* Top toolbar */}
          <Panel position="top-right" className="flex items-center gap-2 m-4">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSettingsOpen(true)}
              title="Error workflow and failure alerts"
            >
              <ShieldAlert className="size-4" />
              On failure
            </Button>
            <Button
              variant="secondary"
              size="sm"
              onClick={saveWorkflow}
              disabled={isSaving || isRunning}
            >
              <Save className="size-4" />
              {isSaving ? "Saving..." : "Save"}
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={handleTestRun}
              disabled={isSaving || isRunning}
            >
              {isRunning && !stepRunning ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
              {isRunning && !stepRunning ? "Running..." : "Test run"}
            </Button>
          </Panel>

          {lastRun && (
            <Panel position="bottom-center" className="mb-4">
              <div
                className={cn(
                  "flex items-center gap-3 rounded-lg border px-3 py-2 text-[12px] shadow-card",
                  lastRun.status === "succeeded"
                    ? "border-success/30 bg-success-bg text-success"
                    : lastRun.status === "failed" || lastRun.status === "canceled"
                      ? "border-danger/30 bg-danger-bg text-danger"
                      : "border-running/30 bg-running-bg text-running",
                )}
              >
                {(lastRun.status === "queued" || lastRun.status === "running") && <Loader2 className="size-3.5 animate-spin" />}
                <span className="font-medium">
                  {lastRun.target ? `Step "${nodes.find((n) => n.id === lastRun.target)?.data.label ?? lastRun.target}"` : "Test run"}
                  {lastRun.number ? ` · run #${lastRun.number}` : ""} · {lastRun.status}
                </span>
                {lastRun.error && <span className="max-w-[360px] truncate text-fg/80">{lastRun.error}</span>}
                {lastRun.id && (
                  <Link href={`/runs/${lastRun.id}`} className="underline underline-offset-2">
                    View run
                  </Link>
                )}
                <button onClick={() => setLastRun(null)} className="opacity-60 hover:opacity-100" aria-label="Dismiss">
                  ×
                </button>
              </div>
            </Panel>
          )}
        </ReactFlow>
      </div>

      {/* Right sidebar - Node Inspector */}
      <NodeInspector
        node={selectedNode}
        deps={selectedDeps}
        samples={samples}
        onUpdateConfig={updateNodeConfig}
        onDelete={deleteSelectedNode}
        onRunStep={(id) => void startTestRun(id)}
        stepRunning={stepRunning}
        busy={isSaving || isRunning}
      />

      {settingsOpen && <WorkflowSettingsDialog slug={workflowSlug} onClose={() => setSettingsOpen(false)} />}

      {runGateOpen && !loginNode && (
        <RunLoginGate
          missing={missingLogins}
          onSignIn={(n) => setLoginNodeId(n.id)}
          onRun={launchTestRun}
          onClose={() => setRunGateOpen(false)}
        />
      )}

      {loginNode && (
        <NodeLoginModal
          node={loginNode}
          onClose={() => setLoginNodeId(null)}
          onConnected={async (type, id) => {
            await refreshCredentials();
            attachCredential(type, id);
            setLoginNodeId(null);
          }}
        />
      )}
    </div>
    </RunDataContext.Provider>
  );
}
