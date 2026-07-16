/**
 * The pieces worker: a Temporal ACTIVITY worker (no workflows) polling the
 * pieces task queue. The Python reasoner's DynamicWorkflow schedules
 * `execute_piece` here by name (docs/pieces-runtime-contract.md §3).
 */
import { NativeConnection, Worker } from '@temporalio/worker';
import { makeExecutePiece } from './execute';
import { loadRegistry } from './pieces';

async function main(): Promise<void> {
  const hostport = process.env.TEMPORAL_HOSTPORT ?? 'localhost:7233';
  const namespace = process.env.TEMPORAL_NAMESPACE ?? 'default';
  const taskQueue = process.env.PIECES_TASK_QUEUE ?? 'agently-pieces';

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

  const connection = await NativeConnection.connect({ address: hostport });
  const worker = await Worker.create({
    connection,
    namespace,
    taskQueue,
    activities: { execute_piece: makeExecutePiece(registry) },
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
