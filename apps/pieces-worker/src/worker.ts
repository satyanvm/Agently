/**
 * The pieces worker: a Temporal ACTIVITY worker (no workflows) polling the
 * pieces task queue. The Python reasoner's DynamicWorkflow schedules
 * `execute_piece` here by name (docs/pieces-runtime-contract.md §3).
 */
import { NativeConnection, Worker } from '@temporalio/worker';
import { loadRepoRootDotEnv, makeDbCredentialResolver } from './credentials';
import { makeExecutePiece } from './execute';
import { startOptionsServer } from './options-server';
import { loadRegistry } from './pieces';
import { makeTriggerStoreFactory } from './trigger-store';

async function main(): Promise<void> {
  // Shared repo-root .env (contract §5) — real env vars always win.
  loadRepoRootDotEnv();

  const hostport = process.env.TEMPORAL_HOSTPORT ?? 'localhost:7233';
  const namespace = process.env.TEMPORAL_NAMESPACE ?? 'default';
  const taskQueue = process.env.PIECES_TASK_QUEUE ?? 'agently-pieces';

  // DB-backed credential resolution (docs/credentials-contract.md §7): reads
  // the same DATABASE_URL the reasoner uses. Without it, credential ids fall
  // back to the env-var path.
  const resolveCredential = makeDbCredentialResolver(process.env.DATABASE_URL);
  if (!resolveCredential) {
    console.warn('DATABASE_URL not set — __credentialId resolution disabled (env-var fallback only)');
  }

  const registry = loadRegistry();
  const actionCount = [...registry.pieces.values()].reduce(
    (n, p) => n + Object.keys(p.actions).length, 0,
  );
  console.log(
    `pieces registry: ${registry.pieces.size} pieces, ${actionCount} actions loaded`,
  );
  for (const f of registry.failures) {
    console.warn(`piece failed to load: ${f.packageName}: ${f.error}`);
  }
  if (registry.pieces.size === 0) {
    console.warn('no pieces installed — execute_piece will fail until packages are added');
  }

  // Interactive HTTP surface: dynamic-prop options + the trigger runtime.
  startOptionsServer(registry, resolveCredential, makeTriggerStoreFactory(process.env.DATABASE_URL));

  const connection = await NativeConnection.connect({ address: hostport });
  const worker = await Worker.create({
    connection,
    namespace,
    taskQueue,
    activities: { execute_piece: makeExecutePiece(registry, resolveCredential) },
  });

  console.log(`pieces worker polling ${taskQueue} @ ${hostport} (ns=${namespace})`);

  const shutdown = () => worker.shutdown();
  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);

  await worker.run();
  await connection.close();
}

main().catch((err) => {
  console.error('pieces worker crashed:', err);
  process.exit(1);
});
