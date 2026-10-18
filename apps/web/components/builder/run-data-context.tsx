"use client";

import * as React from "react";
import type { NodeSamples } from "@/lib/builder-debug";

/** Latest per-node run samples, shared by the canvas (status dots) and the
 *  inspector (Data tab). Provided by WorkflowBuilder. */
export const RunDataContext = React.createContext<NodeSamples | null>(null);

export function useRunData(): NodeSamples | null {
  return React.useContext(RunDataContext);
}
