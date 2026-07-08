#!/usr/bin/env node
// Regenerate the web palette's view of the integration catalog:
//
//   node packages/nodes/build-web.mjs
//
// Reads catalog/*.json (excluding builtin — those stay hand-defined in
// node-catalog.ts with their icons) and emits a single compact JSON module the
// builder imports. Run after editing any cluster file; commit the output.
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const catalogDir = join(here, "catalog");
const outFile = join(here, "..", "..", "apps", "web", "components", "builder", "integration-catalog.generated.json");

const out = [];
for (const name of readdirSync(catalogDir).sort()) {
  if (!name.endsWith(".json") || name === "builtin.json") continue;
  const file = JSON.parse(readFileSync(join(catalogDir, name), "utf8"));
  for (const n of file.nodes ?? []) {
    out.push({
      id: n.id,
      label: n.label,
      description: n.description,
      kind: n.kind,
      runtime: n.runtime,
      cluster: file.cluster,
      clusterLabel: file.label ?? file.cluster,
      config: (n.config ?? []).map(({ key, label, control, placeholder, options, help }) => ({
        key, label, control,
        ...(placeholder ? { placeholder } : {}),
        ...(options ? { options } : {}),
        ...(help ? { help } : {}),
      })),
      credentials: (n.credentials ?? []).map((c) => c.key),
    });
  }
}

writeFileSync(outFile, JSON.stringify(out, null, 1) + "\n");
console.log(`wrote ${out.length} integration nodes → ${outFile}`);
