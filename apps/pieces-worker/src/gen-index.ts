/**
 * Generate packages/nodes/pieces/index.json (contract §2) from the installed
 * piece packages, so the Go planner can plan `pieces.<slug>.<action>` nodes and
 * the Python reasoner can prepare them. Emits both actions and triggers
 * (trigger entries carry kind:"trigger" + strategy). Run via `npm run
 * gen:index` after adding/upgrading piece packages.
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
  // Scalar option values for static_dropdown props, so the web catalog can
  // render a real <select> (credentials-contract §1). Omitted when the piece
  // declares non-scalar option values.
  options?: string[];
}

/** One custom_auth prop, §3 credential-field derivation input. */
interface IndexAuthProp {
  key: string;
  displayName: string;
  type: string;
  required: boolean;
  description?: string;
}

interface IndexAuth {
  type: string;
  credentialKey: string | null;
  required: boolean;
  // Credential-type derivation extras (docs/credentials-contract.md §3): the
  // auth property's own label/help when declared, and for custom_auth the
  // per-field prop definitions the credential form renders.
  displayName?: string;
  description?: string;
  props?: IndexAuthProp[];
}

interface IndexNode {
  id: string;
  piece: string;
  pieceVersion: string;
  // Piece-level metadata the web-catalog generator needs: the human piece name
  // (credential-type labels, §3) and the framework categories (cluster
  // derivation in packages/nodes/build-web.mjs).
  pieceDisplayName: string;
  categories: string[];
  // The action OR trigger name as declared in createAction/createTrigger —
  // one field for both so every index consumer keys the same way.
  action: string;
  label: string;
  description: string;
  kind: 'action' | 'trigger';
  // Trigger nodes only: the framework TriggerStrategy, lower-cased.
  strategy?: 'webhook' | 'polling' | 'app_webhook';
  search: string[];
  auth: IndexAuth;
  props: IndexProp[];
}

const DYNAMIC_PROP_TYPES = new Set(['dropdown', 'multi_select_dropdown', 'dynamic']);

function credentialKeyFor(slug: string): string {
  return `AP_${slug.toUpperCase().replace(/-/g, '_')}_AUTH`;
}

function authBlock(piece: LoadedPiece): IndexAuth {
  const t = String(piece.auth?.type ?? '').toLowerCase();
  if (!t) return { type: 'none', credentialKey: null, required: false };
  const auth: IndexAuth = {
    type: t, // oauth2 | secret_text | basic_auth | custom_auth (framework enum, lower-cased)
    credentialKey: credentialKeyFor(piece.slug),
    required: piece.auth?.required !== false,
  };
  if (piece.auth?.displayName) auth.displayName = String(piece.auth.displayName);
  if ((piece.auth as any)?.description) auth.description = String((piece.auth as any).description);
  if (t === 'custom_auth') {
    const props: IndexAuthProp[] = [];
    for (const [key, def] of Object.entries((piece.auth as any)?.props ?? {})) {
      if (!def || typeof def !== 'object') continue;
      const d = def as Record<string, unknown>;
      const p: IndexAuthProp = {
        key,
        displayName: String(d.displayName ?? key),
        type: String(d.type ?? 'short_text').toLowerCase(),
        required: Boolean(d.required),
      };
      if (d.description) p.description = String(d.description);
      props.push(p);
    }
    auth.props = props;
  }
  return auth;
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
    if (type === 'static_dropdown') {
      const rawOpts = Array.isArray(def.options?.options) ? def.options.options : [];
      const vals = rawOpts
        .map((o: any) => (o && typeof o === 'object' && 'value' in o ? o.value : o))
        .filter((v: any) => ['string', 'number', 'boolean'].includes(typeof v));
      if (vals.length > 0 && vals.length === rawOpts.length) prop.options = vals.map(String);
    }
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

export interface BuildIndexResult {
  nodes: IndexNode[];
  pieceCount: number;
  skipped: Array<{ packageName: string; error: string }>;
}

export function buildIndex(baseDir?: string): BuildIndexResult {
  const registry = loadRegistry(baseDir);
  const nodes: IndexNode[] = [];
  // Import failures were already recorded per-package by loadRegistry; carry
  // them through so the run reports every skipped piece instead of dying.
  const skipped = [...registry.failures];
  for (const piece of [...registry.pieces.values()].sort((a, b) => a.slug.localeCompare(b.slug))) {
    try {
      const seenIds = new Set<string>();
      for (const [name, action] of Object.entries(piece.actions).sort(([a], [b]) => a.localeCompare(b))) {
        const label = String(action.displayName ?? name);
        seenIds.add(`pieces.${piece.slug}.${name}`);
        nodes.push({
          id: `pieces.${piece.slug}.${name}`,
          piece: piece.packageName,
          pieceVersion: piece.version,
          pieceDisplayName: piece.displayName,
          categories: piece.categories,
          action: name,
          label,
          description: String(action.description ?? ''),
          kind: 'action',
          search: searchTerms(piece, label),
          auth: authBlock(piece),
          props: propsOf(action),
        });
      }
      for (const [name, trig] of Object.entries(piece.triggers ?? {}).sort(([a], [b]) => a.localeCompare(b))) {
        const id = `pieces.${piece.slug}.${name}`;
        if (seenIds.has(id)) {
          // Same name declared as action AND trigger — the action keeps the id
          // (the resolution order every consumer already implements).
          skipped.push({ packageName: piece.packageName, error: `trigger "${name}" collides with an action id — skipped` });
          continue;
        }
        const label = String(trig.displayName ?? name);
        const strategy = String(trig.type ?? '').toLowerCase();
        nodes.push({
          id,
          piece: piece.packageName,
          pieceVersion: piece.version,
          pieceDisplayName: piece.displayName,
          categories: piece.categories,
          action: name,
          label,
          description: String(trig.description ?? ''),
          kind: 'trigger',
          ...(strategy === 'webhook' || strategy === 'polling' || strategy === 'app_webhook'
            ? { strategy: strategy as IndexNode['strategy'] }
            : {}),
          search: searchTerms(piece, label),
          auth: authBlock(piece),
          props: propsOf(trig),
        });
      }
    } catch (err) {
      // One malformed piece must not sink the whole index run.
      skipped.push({ packageName: piece.packageName, error: String(err) });
    }
  }
  return { nodes, pieceCount: registry.pieces.size, skipped };
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
  const { nodes, pieceCount, skipped } = buildIndex();
  for (const s of skipped) {
    console.error(`piece index generation failed for ${s.packageName}: ${s.error.split('\n')[0]}`);
  }
  if (skipped.length > 0) throw new Error(`piece index generation aborted: ${skipped.length} package/action errors`);
  const outDir = path.join(repoRoot(), 'packages', 'nodes', 'pieces');
  fs.mkdirSync(outDir, { recursive: true });
  const outPath = path.join(outDir, 'index.json');
  fs.writeFileSync(
    outPath,
    JSON.stringify({ version: 1, generatedAt: new Date().toISOString(), nodes }, null, 2) + '\n',
  );
  const triggerCount = nodes.filter((n) => n.kind === 'trigger').length;
  console.log(
    `wrote ${outPath}: ${nodes.length - triggerCount} actions + ${triggerCount} triggers from ${pieceCount} pieces` +
    (skipped.length ? ` (${skipped.length} skipped)` : ''),
  );
}
