/**
 * Generate packages/nodes/pieces/embeddings.{json,bin} — the offline half of
 * the planner's embedding prefilter (apps/api piecesembed.go). One vector per
 * index node (action or trigger), embedded with Gemini as RETRIEVAL_DOCUMENT
 * and unit-normalized so the Go side's dot product equals cosine similarity.
 *
 * Incremental: rows whose (model, dims, text) hash matches the existing
 * manifest are reused without an API call, so re-runs after small catalog
 * changes are near-free. On a fatal error (typically quota exhaustion) the
 * vectors embedded so far are saved to embeddings.partial.{json,bin} — a
 * staging file the Go loader never reads — so re-running after the quota
 * window converges to a complete sidecar instead of starting over. Run via
 * `npm run gen:embeddings` after gen:index; requires GEMINI_API_KEY (or
 * GOOGLE_API_KEY).
 */
import * as crypto from 'node:crypto';
import * as fs from 'node:fs';
import * as path from 'node:path';

const MODEL = process.env.PLANNER_EMBED_MODEL || 'gemini-embedding-001';
const DIMS = Number(process.env.PLANNER_EMBED_DIMS || 768);
const BATCH = 100; // batchEmbedContents request cap
const MAX_RETRIES = 8; // waits total ~2min — rides out per-minute quota windows

interface IndexNode {
  id: string;
  pieceDisplayName?: string;
  label?: string;
  description?: string;
  search?: string[];
}

interface Manifest {
  version: number;
  model: string;
  dims: number;
  generatedAt: string;
  ids: string[];
  hashes: Record<string, string>;
}

/** The text a node is embedded as: service name + action label + description
 * + search terms — the same capability surface the router directory shows. */
function embedText(n: IndexNode): string {
  const parts = [
    `${n.pieceDisplayName ?? ''} — ${n.label ?? n.id}`.trim(),
    (n.description ?? '').trim(),
    (n.search ?? []).join(' '),
  ].filter(Boolean);
  return parts.join('. ');
}

function hashOf(text: string): string {
  return crypto.createHash('sha256').update(`${MODEL}|${DIMS}|${text}`).digest('hex').slice(0, 16);
}

function normalize(v: number[]): Float32Array {
  let norm = 0;
  for (const x of v) norm += x * x;
  const scale = norm > 0 ? 1 / Math.sqrt(norm) : 0;
  const out = new Float32Array(v.length);
  for (let i = 0; i < v.length; i++) out[i] = v[i] * scale;
  return out;
}

async function embedBatch(key: string, texts: string[]): Promise<Float32Array[]> {
  const body = JSON.stringify({
    requests: texts.map((text) => ({
      model: `models/${MODEL}`,
      content: { parts: [{ text }] },
      taskType: 'RETRIEVAL_DOCUMENT',
      outputDimensionality: DIMS,
    })),
  });
  const url = `https://generativelanguage.googleapis.com/v1beta/models/${MODEL}:batchEmbedContents`;
  for (let attempt = 0; ; attempt++) {
    let resp: Response;
    try {
      resp = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'x-goog-api-key': key },
        body,
      });
    } catch (err) {
      // Transient network failure — same backoff as a 429.
      if (attempt < MAX_RETRIES) {
        const wait = Math.min(30_000, 1_000 * 2 ** attempt);
        console.warn(`fetch failed (${String(err).slice(0, 80)}); retrying in ${wait / 1000}s`);
        await new Promise((r) => setTimeout(r, wait));
        continue;
      }
      throw err;
    }
    if (resp.ok) {
      const parsed = (await resp.json()) as { embeddings?: Array<{ values?: number[] }> };
      const rows = parsed.embeddings ?? [];
      if (rows.length !== texts.length) {
        throw new Error(`batch returned ${rows.length} embeddings for ${texts.length} texts`);
      }
      return rows.map((r) => {
        if (!Array.isArray(r.values) || r.values.length !== DIMS) {
          throw new Error(`embedding has ${r.values?.length ?? 0} dims, want ${DIMS}`);
        }
        return normalize(r.values);
      });
    }
    // 429/5xx are transient (rate limits, hiccups); everything else is fatal.
    if ((resp.status === 429 || resp.status >= 500) && attempt < MAX_RETRIES) {
      const wait = Math.min(30_000, 1_000 * 2 ** attempt);
      console.warn(`gemini ${resp.status}; retrying in ${wait / 1000}s`);
      await new Promise((r) => setTimeout(r, wait));
      continue;
    }
    throw new Error(`gemini batchEmbedContents status ${resp.status}: ${(await resp.text()).slice(0, 300)}`);
  }
}

function repoRoot(): string {
  let dir = __dirname;
  for (let i = 0; i < 8; i++) {
    if (fs.existsSync(path.join(dir, 'packages', 'nodes'))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error('could not locate repo root (packages/nodes) above ' + __dirname);
}

/** Read a prior sidecar (or staging file) into an id|hash → vector reuse map.
 * dims/model mismatch or size inconsistency just means nothing is reusable. */
function loadReusable(manifestPath: string, binPath: string, into: Map<string, Float32Array>): void {
  if (!fs.existsSync(manifestPath) || !fs.existsSync(binPath)) return;
  try {
    const m = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as Manifest;
    if (m.model !== MODEL || m.dims !== DIMS) return;
    const bin = fs.readFileSync(binPath);
    if (bin.length !== m.ids.length * DIMS * 4) return;
    m.ids.forEach((id, i) => {
      into.set(`${id}|${m.hashes[id] ?? ''}`, new Float32Array(bin.buffer, bin.byteOffset + i * DIMS * 4, DIMS));
    });
  } catch {
    // A corrupt file just means a full re-embed.
  }
}

/** Write ids' vectors as manifest + bin. Rows must all be present in vectors. */
function writeSidecar(manifestPath: string, binPath: string, ids: string[], hashes: Record<string, string>, vectors: Map<string, Float32Array>): number {
  const bin = Buffer.alloc(ids.length * DIMS * 4);
  ids.forEach((id, i) => {
    const v = vectors.get(id);
    if (!v) throw new Error(`missing vector for ${id}`);
    for (let j = 0; j < DIMS; j++) bin.writeFloatLE(v[j], (i * DIMS + j) * 4);
  });
  const manifest: Manifest = {
    version: 1,
    model: MODEL,
    dims: DIMS,
    generatedAt: new Date().toISOString(),
    ids,
    hashes,
  };
  fs.writeFileSync(binPath, bin);
  fs.writeFileSync(manifestPath, JSON.stringify(manifest) + '\n');
  return bin.length;
}

async function main(): Promise<void> {
  const key = process.env.GEMINI_API_KEY || process.env.GOOGLE_API_KEY;
  if (!key) {
    console.error('GEMINI_API_KEY (or GOOGLE_API_KEY) is required to generate embeddings');
    process.exit(1);
  }

  const piecesDir = path.join(repoRoot(), 'packages', 'nodes', 'pieces');
  const index = JSON.parse(fs.readFileSync(path.join(piecesDir, 'index.json'), 'utf8')) as {
    nodes: IndexNode[];
  };
  const nodes = index.nodes.filter((n) => n.id);

  const manifestPath = path.join(piecesDir, 'embeddings.json');
  const binPath = path.join(piecesDir, 'embeddings.bin');
  const partialManifestPath = path.join(piecesDir, 'embeddings.partial.json');
  const partialBinPath = path.join(piecesDir, 'embeddings.partial.bin');

  // Reuse from the last complete sidecar AND any staged partial run.
  const prev = new Map<string, Float32Array>();
  loadReusable(manifestPath, binPath, prev);
  loadReusable(partialManifestPath, partialBinPath, prev);

  const hashes: Record<string, string> = {};
  const vectors = new Map<string, Float32Array>();
  const pending: Array<{ id: string; text: string }> = [];
  for (const n of nodes) {
    const text = embedText(n);
    const h = hashOf(text);
    hashes[n.id] = h;
    const reused = prev.get(`${n.id}|${h}`);
    if (reused) {
      vectors.set(n.id, reused);
    } else {
      pending.push({ id: n.id, text });
    }
  }
  console.log(`${nodes.length} nodes: ${nodes.length - pending.length} reused, ${pending.length} to embed`);

  try {
    for (let i = 0; i < pending.length; i += BATCH) {
      const batch = pending.slice(i, i + BATCH);
      const embedded = await embedBatch(key, batch.map((b) => b.text));
      batch.forEach((b, j) => vectors.set(b.id, embedded[j]));
      console.log(`embedded ${Math.min(i + BATCH, pending.length)}/${pending.length}`);
    }
  } catch (err) {
    // Quota/network death mid-run: stage what we have (the Go loader only ever
    // reads the complete sidecar, so a partial file can never skew routing) and
    // leave the previous complete sidecar untouched.
    const done = nodes.filter((n) => vectors.has(n.id)).map((n) => n.id);
    if (done.length > 0) {
      writeSidecar(partialManifestPath, partialBinPath, done, hashes, vectors);
      console.error(`staged ${done.length}/${nodes.length} vectors to ${partialManifestPath}; re-run gen:embeddings to resume`);
    }
    throw err;
  }

  const ids = nodes.map((n) => n.id);
  const size = writeSidecar(manifestPath, binPath, ids, hashes, vectors);
  for (const p of [partialManifestPath, partialBinPath]) {
    if (fs.existsSync(p)) fs.unlinkSync(p);
  }
  console.log(`wrote ${binPath} (${(size / 1024 / 1024).toFixed(1)} MB) + ${manifestPath}`);
}

if (require.main === module) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
