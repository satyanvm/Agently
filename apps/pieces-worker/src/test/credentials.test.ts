import * as assert from 'node:assert/strict';
import { test } from 'node:test';
import { openCredentialData } from '../credentials';

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
