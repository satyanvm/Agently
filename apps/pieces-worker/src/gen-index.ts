/**
 * Generate packages/nodes/pieces/index.json (contract §2) from the installed
 * piece packages, so the Go planner can plan `pieces.<slug>.<action>` nodes and
 * the Python reasoner can prepare them. Actions only — triggers are a later
 * phase. Run via `npm run gen:index` after adding/upgrading piece packages.
 */
import * as fs from 'node:fs';
import * as path from 'node:path';
import { loadRegistry, LoadedPiece } from './pieces';

interface IndexProp {
  key: string;
  label: string;
  type: string;
  required: boolean;
  description?: string;
  dynamic?: boolean;
}

interface IndexNode {
  id: string;
  piece: string;
  pieceVersion: string;
  action: string;
  label: string;
  description: string;
  kind: 'action';
  search: string[];
  auth: { type: string; credentialKey: string | null; required: boolean };
  props: IndexProp[];
}

const DYNAMIC_PROP_TYPES = new Set(['dropdown', 'multi_select_dropdown', 'dynamic']);

function credentialKeyFor(slug: string): string {
  return `AP_${slug.toUpperCase().replace(/-/g, '_')}_AUTH`;
}

function authBlock(piece: LoadedPiece): IndexNode['auth'] {
  const t = String(piece.auth?.type ?? '').toLowerCase();
  if (!t) return { type: 'none', credentialKey: null, required: false };
  return {
    type: t, // oauth2 | secret_text | basic_auth | custom_auth (framework enum, lower-cased)
    credentialKey: credentialKeyFor(piece.slug),
    required: piece.auth?.required !== false,
  };
}

function propsOf(action: { props?: Record<string, any> }): IndexProp[] {
  const out: IndexProp[] = [];
  for (const [key, def] of Object.entries(action.props ?? {})) {
    if (!def || typeof def !== 'object') continue;
    const type = String(def.type ?? 'short_text').toLowerCase();
    const prop: IndexProp = {
      key,
      label: String(def.displayName ?? key),
      type,
      required: Boolean(def.required),
    };
    if (def.description) prop.description = String(def.description);
    if (DYNAMIC_PROP_TYPES.has(type)) prop.dynamic = true;
    out.push(prop);
  }
  return out;
}

function searchTerms(piece: LoadedPiece, actionLabel: string): string[] {
  const words = new Set<string>();
  for (const w of `${piece.slug.replace(/-/g, ' ')} ${actionLabel}`.toLowerCase().split(/\s+/)) {
    if (w.length > 2) words.add(w);
  }
  return [...words];
}

export function buildIndex(baseDir?: string): { nodes: IndexNode[]; pieceCount: number } {
  const registry = loadRegistry(baseDir);
  const nodes: IndexNode[] = [];
  for (const piece of [...registry.pieces.values()].sort((a, b) => a.slug.localeCompare(b.slug))) {
    for (const [name, action] of Object.entries(piece.actions).sort(([a], [b]) => a.localeCompare(b))) {
      const label = String(action.displayName ?? name);
      nodes.push({
        id: `pieces.${piece.slug}.${name}`,
        piece: piece.packageName,
        pieceVersion: piece.version,
        action: name,
        label,
        description: String(action.description ?? ''),
        kind: 'action',
        search: searchTerms(piece, label),
        auth: authBlock(piece),
        props: propsOf(action),
      });
    }
  }
  return { nodes, pieceCount: registry.pieces.size };
}

function repoRoot(): string {
  // apps/pieces-worker/dist/gen-index.js → repo root is three levels up.
  let dir = __dirname;
  for (let i = 0; i < 8; i++) {
    if (fs.existsSync(path.join(dir, 'packages', 'nodes'))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error('could not locate repo root (packages/nodes) above ' + __dirname);
}

if (require.main === module) {
  const { nodes, pieceCount } = buildIndex();
  const outDir = path.join(repoRoot(), 'packages', 'nodes', 'pieces');
  fs.mkdirSync(outDir, { recursive: true });
  const outPath = path.join(outDir, 'index.json');
  fs.writeFileSync(
    outPath,
    JSON.stringify({ version: 1, generatedAt: new Date().toISOString(), nodes }, null, 2) + '\n',
  );
  console.log(`wrote ${outPath}: ${nodes.length} actions from ${pieceCount} pieces`);
}
