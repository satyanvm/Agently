/**
 * Piece registry: load every installed `@activepieces/piece-*` npm package and
 * index its actions.
 *
 * Pieces are used AS A LIBRARY (docs/pieces-runtime-contract.md): each package
 * exports a `Piece` instance built with createPiece(). The exporting name varies
 * (`slack`, `stripe`, `googleSheets`, …) and the framework class identity varies
 * with each package's own pinned pieces-framework, so detection is duck-typed —
 * an exported object with a string `displayName` and an `actions()` method is
 * the piece. The vendored monorepo under external/activepieces is reference
 * only and never imported.
 */
import * as fs from 'node:fs';
import * as path from 'node:path';

export interface PieceAuthProp {
  type?: string; // PropertyType: OAUTH2 | SECRET_TEXT | BASIC_AUTH | CUSTOM_AUTH | ...
  required?: boolean;
  displayName?: string;
  [key: string]: unknown;
}

export interface PieceAction {
  name: string;
  displayName?: string;
  description?: string;
  requireAuth?: boolean;
  props?: Record<string, any>;
  run: (ctx: any) => Promise<unknown>;
}

export interface LoadedPiece {
  slug: string; // "google-sheets" from "@activepieces/piece-google-sheets"
  packageName: string; // "@activepieces/piece-google-sheets"
  version: string;
  displayName: string;
  description: string;
  auth: PieceAuthProp | undefined; // first auth when the piece declares several
  actions: Record<string, PieceAction>;
}

export interface Registry {
  pieces: Map<string, LoadedPiece>; // slug → piece
  failures: Array<{ packageName: string; error: string }>;
}

const PIECE_PKG_PREFIX = '@activepieces/piece-';

/** Duck-type check for a Piece instance across framework versions. */
function looksLikePiece(v: unknown): v is { displayName: string; actions: () => Record<string, PieceAction> } {
  return (
    typeof v === 'object' && v !== null &&
    typeof (v as any).displayName === 'string' &&
    typeof (v as any).actions === 'function'
  );
}

/** Find the Piece instance among a module's exports (named or default). */
export function resolvePieceExport(mod: Record<string, unknown>): { displayName: string; actions: () => Record<string, PieceAction> } | null {
  const candidates = [mod.default, ...Object.values(mod)];
  for (const v of candidates) {
    if (looksLikePiece(v)) return v;
  }
  return null;
}

/** Normalize the piece's auth property: newer frameworks allow an array. */
function firstAuth(auth: unknown): PieceAuthProp | undefined {
  if (Array.isArray(auth)) return auth[0] as PieceAuthProp | undefined;
  return (auth as PieceAuthProp) ?? undefined;
}

export function slugOf(packageName: string): string {
  return packageName.slice(PIECE_PKG_PREFIX.length);
}

/**
 * Scan node_modules for installed piece packages and load them. A package that
 * fails to import is recorded and skipped — one broken piece must not take the
 * worker down.
 */
export function loadRegistry(baseDir: string = __dirname): Registry {
  const registry: Registry = { pieces: new Map(), failures: [] };
  const scopeDir = findScopeDir(baseDir);
  if (!scopeDir) return registry;

  for (const entry of fs.readdirSync(scopeDir).sort()) {
    if (!entry.startsWith('piece-')) continue;
    const packageName = `@activepieces/${entry}`;
    try {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const mod = require(path.join(scopeDir, entry));
      const piece = resolvePieceExport(mod);
      if (!piece) {
        registry.failures.push({ packageName, error: 'no Piece export found' });
        continue;
      }
      const pkgJson = JSON.parse(
        fs.readFileSync(path.join(scopeDir, entry, 'package.json'), 'utf8'),
      );
      registry.pieces.set(slugOf(packageName), {
        slug: slugOf(packageName),
        packageName,
        version: String(pkgJson.version ?? '0.0.0'),
        displayName: piece.displayName,
        description: String((piece as any).description ?? ''),
        auth: firstAuth((piece as any).auth),
        actions: piece.actions(),
      });
    } catch (err) {
      registry.failures.push({ packageName, error: String(err) });
    }
  }
  return registry;
}

/** Walk up from baseDir to the nearest node_modules/@activepieces. */
function findScopeDir(baseDir: string): string | null {
  let dir = baseDir;
  for (let i = 0; i < 8; i++) {
    const cand = path.join(dir, 'node_modules', '@activepieces');
    if (fs.existsSync(cand) && fs.statSync(cand).isDirectory()) return cand;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

export function actionOf(registry: Registry, slug: string, actionName: string): { piece: LoadedPiece; action: PieceAction } | null {
  const piece = registry.pieces.get(slug);
  if (!piece) return null;
  const action = piece.actions[actionName];
  if (!action) return null;
  return { piece, action };
}
