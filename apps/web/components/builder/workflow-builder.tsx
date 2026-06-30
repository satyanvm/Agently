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
import { Save, Play, Undo, Redo, Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { NodeCatalog } from "./node-catalog-panel";
import { NodeInspector } from "./node-inspector";
import { WorkflowNode } from "./workflow-node";
import { NODE_CATALOG, defaultConfig, findNodeSpec } from "./node-catalog";
import { getWorkflowGraph, saveWorkflowGraph } from "@/lib/api";
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

export function WorkflowBuilder({ workflowSlug }: WorkflowBuilderProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowNodeType>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [selectedNodeId, setSelectedNodeId] = React.useState<string | null>(null);
  const [isSaving, setIsSaving] = React.useState(false);
  const reactFlowWrapper = React.useRef<HTMLDivElement>(null);
  const [reactFlowInstance, setReactFlowInstance] = React.useState<any>(null);

  // Load workflow graph on mount
  React.useEffect(() => {
    loadWorkflow();
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

  const saveWorkflow = async () => {
    setIsSaving(true);
    try {
      await saveWorkflowGraph(workflowSlug, { nodes, edges });
    } catch (err) {
      console.error("Failed to save workflow:", err);
    } finally {
      setIsSaving(false);
    }
  };

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
    },
    [reactFlowInstance, setNodes]
  );

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

  return (
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
            color="rgba(17, 18, 38, 0.08)"
          />
          <Controls
            className="!border-border !bg-surface !shadow-card [&_button]:!border-border [&_button]:!bg-surface-2 [&_button:hover]:!bg-surface-3"
            showInteractive={false}
          />

          {/* Top toolbar */}
          <Panel position="top-right" className="flex items-center gap-2 m-4">
            <Button variant="secondary" size="sm" onClick={saveWorkflow} disabled={isSaving}>
              <Save className="size-4" />
              {isSaving ? "Saving..." : "Save"}
            </Button>
            <Button variant="primary" size="sm">
              <Play className="size-4" />
              Test run
            </Button>
          </Panel>
        </ReactFlow>
      </div>

      {/* Right sidebar - Node Inspector */}
      <NodeInspector
        node={selectedNode}
        onUpdateConfig={updateNodeConfig}
        onDelete={deleteSelectedNode}
      />
    </div>
  );
}
