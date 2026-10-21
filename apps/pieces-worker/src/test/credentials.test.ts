import * as assert from 'node:assert/strict';
import { test } from 'node:test';
import { makeOAuthRefresher, oauthClient, openCredentialData } from '../credentials';

const KEY = '000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f';
// Sealed by the API's platform.sealCredentialData under KEY.
const SEALED = { $enc: 'v1:6wvhxAZvcovFUtWBtR7EGXe9usisTno4XQzaftondT0Vu8bfhaKqXjTqqbgWcG06' };

test('openCredentialData opens an API-sealed value', () => {
  assert.deepEqual(openCredentialData(SEALED, KEY), { token: 'xoxb-123' });
});

test('openCredentialData passes legacy plaintext through', () => {
  assert.deepEqual(openCredentialData({ token: 'plain' }, KEY), { token: 'plain' });
});

test('openCredentialData fails without a key or with the wrong key', () => {
  assert.throws(() => openCredentialData(SEALED, ''), /not set/);
  assert.throws(() => openCredentialData(SEALED, 'ff'.repeat(32)), /could not be decrypted/);
});

test('makeOAuthRefresher swaps an expired access token and caches it', async () => {
  const calls: Array<{ url: string; body: string }> = [];
  const fakeFetch = (async (url: string, init: { body: string }) => {
    calls.push({ url, body: init.body });
    return { ok: true, json: async () => ({ access_token: 'fresh', expires_in: 3600 }) };
  }) as unknown as typeof fetch;
  const env = { OAUTH_ACME_CLIENT_ID: 'cid', OAUTH_ACME_CLIENT_SECRET: 'sec' };
  let t = 10_000;
  const refresh = makeOAuthRefresher(fakeFetch, env, () => t);
  const stored = { access_token: 'stale', refresh_token: 'rt', token_url: 'https://acme.test/token', expires_at: 9_000 };

  const out = await refresh('cred1', 'pieces.acme', stored);
  assert.equal(out.access_token, 'fresh');
  assert.equal(calls.length, 1);
  assert.match(calls[0].body, /grant_type=refresh_token/);
  assert.match(calls[0].body, /client_secret=sec/);

  t += 100; // still valid → served from cache, no second call
  assert.equal((await refresh('cred1', 'pieces.acme', stored)).access_token, 'fresh');
  assert.equal(calls.length, 1);
});

test('makeOAuthRefresher leaves valid or non-oauth credentials alone', async () => {
  const fakeFetch = (async () => {
    throw new Error('should not be called');
  }) as unknown as typeof fetch;
  const refresh = makeOAuthRefresher(fakeFetch, {}, () => 1_000);
  const valid = { access_token: 'ok', refresh_token: 'rt', token_url: 'https://x/t', expires_at: 5_000 };
  assert.equal((await refresh('a', 'pieces.acme', valid)).access_token, 'ok');
  assert.deepEqual(await refresh('b', 'pieces.acme', { value: 'key' }), { value: 'key' });
  // Expired but no client configured → stored token passes through.
  const expired = { ...valid, expires_at: 10 };
  assert.equal((await refresh('c', 'pieces.acme', expired)).access_token, 'ok');
});

test('oauthClient falls back to the shared Google app', () => {
  assert.deepEqual(oauthClient('pieces.google-sheets', { GOOGLE_CLIENT_ID: 'g', GOOGLE_CLIENT_SECRET: 's' }), ['g', 's']);
  assert.deepEqual(oauthClient('pieces.slack', { GOOGLE_CLIENT_ID: 'g', GOOGLE_CLIENT_SECRET: 's' }), ['', '']);
});
