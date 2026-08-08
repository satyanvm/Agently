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

  const databaseUrl = process.env.DATABASE_URL?.trim();
  if (!databaseUrl) {
    throw new Error('DATABASE_URL is required — set it in the repository .env');
  }
  const resolveCredential = makeDbCredentialResolver(databaseUrl);

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
  if (registry.failures.length > 0) {
    throw new Error(`pieces registry failed to load ${registry.failures.length} package(s)`);
  }
  if (registry.pieces.size === 0) {
    throw new Error('no Activepieces packages installed — build the pieces registry before starting');
  }

  // Interactive HTTP surface: dynamic-prop options + the trigger runtime.
  startOptionsServer(registry, resolveCredential, makeTriggerStoreFactory(databaseUrl));

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
