/**
 * Unit tests for the pieces worker — no Temporal server, no live APIs, no
 * requirement that real piece packages are installed (a fake registry stands
 * in). `npm test` runs these via node:test against the compiled dist/.
 */
import * as assert from 'node:assert/strict';
import { test } from 'node:test';
import { jsonSafe, makeExecutePiece, normalizeAuth, normalizeDbAuth, withDefaults } from '../execute';
import { LoadedPiece, Registry, resolvePieceExport } from '../pieces';
import { buildIndex } from '../gen-index';

function fakePiece(overrides: Partial<LoadedPiece> = {}): LoadedPiece {
  return {
    slug: 'fake',
    packageName: '@activepieces/piece-fake',
    version: '1.2.3',
    displayName: 'Fake',
    description: 'A fake piece',
    categories: [],
    auth: { type: 'SECRET_TEXT', required: true },
    triggers: {},
    actions: {
      echo: {
        name: 'echo',
        displayName: 'Echo',
        description: 'Echoes props',
        props: { text: { type: 'SHORT_TEXT', displayName: 'Text', required: true } },
        run: async (ctx: any) => ({ echoed: ctx.propsValue.text, hadAuth: Boolean(ctx.auth) }),
      },
      boom: {
        name: 'boom',
        displayName: 'Boom',
        description: 'Always throws',
        props: {},
        run: async () => {
          throw new Error('channel_not_found');
        },
      },
      wants_platform: {
        name: 'wants_platform',
        displayName: 'Wants platform',
        description: 'Touches unsupported platform services',
        props: {},
        run: async (ctx: any) => ctx.run.createWaitpoint({ type: 'DELAY' }),
      },
      stateful: {
        name: 'stateful',
        displayName: 'Stateful',
        description: 'Uses the store',
        props: {},
        run: async (ctx: any) => {
          await ctx.store.put('k', 41);
          const v = (await ctx.store.get('k')) as number;
          return { v: v + 1 };
        },
      },
    },
    ...overrides,
  };
}

function registryWith(...pieces: LoadedPiece[]): Registry {
  return {
    pieces: new Map(pieces.map((p) => [p.slug, p])),
    failures: [],
  };
}

test('resolvePieceExport finds duck-typed piece among exports', () => {
  const piece = { displayName: 'X', actions: () => ({}) };
  assert.equal(resolvePieceExport({ notIt: 42, x: piece }), piece);
  assert.equal(resolvePieceExport({ default: piece }), piece);
  assert.equal(resolvePieceExport({ nothing: 1 }), null);
});

test('execute_piece runs an action end-to-end', async () => {
  process.env.AP_FAKE_AUTH = 'sek';
  const exec = makeExecutePiece(registryWith(fakePiece()));
  const res = await exec({
    piece: '@activepieces/piece-fake',
    action: 'echo',
    props: { text: 'hello' },
    authEnvKey: 'AP_FAKE_AUTH',
  });
  assert.deepEqual(res, { ok: true, output: { echoed: 'hello', hadAuth: true } });
  delete process.env.AP_FAKE_AUTH;
});

test('piece throw is RETURNED as ok:false, not thrown', async () => {
  const exec = makeExecutePiece(registryWith(fakePiece()));
  const res = await exec({
    piece: '@activepieces/piece-fake',
    action: 'boom',
    props: {},
    authEnvKey: null,
  });
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.match(res.error, /channel_not_found/);
    assert.equal(res.errorType, 'PieceExecutionError');
  }
});

test('unknown piece/action THROWS (retryable infra error)', async () => {
  const exec = makeExecutePiece(registryWith(fakePiece()));
  await assert.rejects(
    exec({ piece: '@activepieces/piece-ghost', action: 'x', props: {} }),
    /not installed/,
  );
  await assert.rejects(
    exec({ piece: '@activepieces/piece-fake', action: 'ghost_action', props: {} }),
    /not installed/,
  );
});

test('missing credential env returns MissingCredential, not throw', async () => {
  delete process.env.AP_FAKE_AUTH;
  const exec = makeExecutePiece(registryWith(fakePiece()));
  const res = await exec({
    piece: '@activepieces/piece-fake',
    action: 'echo',
    props: { text: 'x' },
    authEnvKey: 'AP_FAKE_AUTH',
  });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.errorType, 'MissingCredential');
});

test('credentialId resolves via the DB resolver and wins over env', async () => {
  process.env.AP_FAKE_AUTH = 'env-secret';
  const exec = makeExecutePiece(
    registryWith(fakePiece()),
    async (id: string) => (id === 'cred_1' ? { value: 'db-secret' } : null),
  );
  const res = await exec({
    piece: '@activepieces/piece-fake',
    action: 'echo',
    props: { text: 'x' },
    authEnvKey: 'AP_FAKE_AUTH',
    credentialId: 'cred_1',
  });
  assert.deepEqual(res, { ok: true, output: { echoed: 'x', hadAuth: true } });
  delete process.env.AP_FAKE_AUTH;
});

test('dangling credentialId falls back to env; missing both → MissingCredential', async () => {
  const resolver = async () => null; // row deleted
  process.env.AP_FAKE_AUTH = 'env-secret';
  const exec = makeExecutePiece(registryWith(fakePiece()), resolver);
  const withEnv = await exec({
    piece: '@activepieces/piece-fake',
    action: 'echo',
    props: { text: 'x' },
    authEnvKey: 'AP_FAKE_AUTH',
    credentialId: 'cred_gone',
  });
  assert.equal(withEnv.ok, true);

  delete process.env.AP_FAKE_AUTH;
  const withoutEnv = await exec({
    piece: '@activepieces/piece-fake',
    action: 'echo',
    props: { text: 'x' },
    authEnvKey: 'AP_FAKE_AUTH',
    credentialId: 'cred_gone',
  });
  assert.equal(withoutEnv.ok, false);
  if (!withoutEnv.ok) assert.equal(withoutEnv.errorType, 'MissingCredential');
});

test('normalizeDbAuth shapes DB rows per credentials-contract §7', () => {
  const secret = normalizeDbAuth({ value: 'sk-1' }, fakePiece()) as any;
  assert.equal(`Bearer ${secret}`, 'Bearer sk-1');
  assert.equal(secret.secret_text, 'sk-1');

  const basic = normalizeDbAuth(
    { username: 'me', password: 'pw' },
    fakePiece({ auth: { type: 'BASIC_AUTH', required: true } }),
  );
  assert.deepEqual(basic, { username: 'me', password: 'pw' });

  const custom = normalizeDbAuth(
    { apiKey: 'k' },
    fakePiece({ auth: { type: 'CUSTOM_AUTH', required: true } }),
  ) as any;
  assert.equal(custom.apiKey, 'k');
  assert.equal(custom.props.apiKey, 'k');

  const oauth = normalizeDbAuth(
    { access_token: 'tok', refresh_token: 'ref' },
    fakePiece({ auth: { type: 'OAUTH2', required: true } }),
  ) as any;
  assert.equal(oauth.access_token, 'tok');
  assert.equal(oauth.refresh_token, 'ref');
});

test('unsupported platform features surface as ok:false UnsupportedPieceFeature', async () => {
  const exec = makeExecutePiece(registryWith(fakePiece()));
  const res = await exec({
    piece: '@activepieces/piece-fake',
    action: 'wants_platform',
    props: {},
    authEnvKey: null,
  });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.errorType, 'UnsupportedPieceFeature');
});

test('in-memory store works within one invocation', async () => {
  const exec = makeExecutePiece(registryWith(fakePiece()));
  const res = await exec({
    piece: '@activepieces/piece-fake',
    action: 'stateful',
    props: {},
    authEnvKey: null,
  });
  assert.deepEqual(res, { ok: true, output: { v: 42 } });
});

test('normalizeAuth: bare string for oauth2 piece → access_token object', () => {
  const piece = fakePiece({ auth: { type: 'OAUTH2', required: true } });
  const auth = normalizeAuth('tok-123', piece) as { access_token: string };
  assert.equal(auth.access_token, 'tok-123');
});

test('normalizeAuth: secret_text works as string AND as .secret_text', () => {
  const piece = fakePiece({ auth: { type: 'SECRET_TEXT', required: true } });
  const auth = normalizeAuth('sk-live-1', piece) as any;
  assert.equal(`Bearer ${auth}`, 'Bearer sk-live-1'); // string interpolation
  assert.equal(auth.secret_text, 'sk-live-1'); // property access
});

test('normalizeAuth: basic user:pass split; JSON object passthrough', () => {
  const basic = fakePiece({ auth: { type: 'BASIC_AUTH', required: true } });
  assert.deepEqual(normalizeAuth('me:pw', basic), { username: 'me', password: 'pw' });

  const custom = fakePiece({ auth: { type: 'CUSTOM_AUTH', required: true } });
  const obj = normalizeAuth('{"apiKey":"k","domain":"d"}', custom) as any;
  assert.equal(obj.apiKey, 'k');
  assert.equal(obj.props.apiKey, 'k'); // dual access for custom auth
});

test('withDefaults applies declared prop defaults', () => {
  const out = withDefaults(
    { a: 1 },
    { a: { defaultValue: 9 }, b: { defaultValue: 'x' }, c: {} },
  );
  assert.deepEqual(out, { a: 1, b: 'x' });
});

test('jsonSafe: undefined → null, bigint stringified, cycles degrade', () => {
  assert.equal(jsonSafe(undefined), null);
  assert.deepEqual(jsonSafe({ n: 1n }), { n: '1' });
  const cyc: any = {};
  cyc.self = cyc;
  assert.equal(typeof jsonSafe(cyc), 'string');
});

test('buildIndex emits contract-conformant nodes for installed pieces', () => {
  const { nodes } = buildIndex();
  // Works with zero pieces installed (empty index) — but every node present
  // must satisfy the contract schema.
  for (const n of nodes) {
    // Action names are VERBATIM from createAction (contract §1) and a handful
    // of community pieces use spaces/caps instead of snake_case — ids are
    // opaque strings everywhere downstream, so only require a non-empty tail.
    assert.match(n.id, /^pieces\.[a-z0-9-]+\..+$/);
    assert.match(n.piece, /^@activepieces\/piece-/);
    assert.ok(n.kind === 'action' || n.kind === 'trigger', `unexpected kind ${n.kind} on ${n.id}`);
    if (n.kind === 'trigger' && n.strategy !== undefined) {
      assert.ok(['webhook', 'polling', 'app_webhook'].includes(n.strategy),
        `unexpected strategy ${n.strategy} on ${n.id}`);
    }
    assert.equal(typeof n.label, 'string');
    assert.ok(Array.isArray(n.search));
    assert.ok(['oauth2', 'secret_text', 'basic_auth', 'custom_auth', 'none'].includes(n.auth.type),
      `unexpected auth type ${n.auth.type} on ${n.id}`);
    if (n.auth.type === 'none') {
      assert.equal(n.auth.credentialKey, null);
    } else {
      assert.match(String(n.auth.credentialKey), /^AP_[A-Z0-9_]+_AUTH$/);
    }
    for (const p of n.props) {
      assert.equal(typeof p.key, 'string');
      assert.equal(typeof p.required, 'boolean');
      assert.equal(p.type, p.type.toLowerCase());
    }
  }
});
