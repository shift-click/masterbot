import test from 'node:test';
import assert from 'node:assert/strict';

import {
  CACHE_MAX_ENTRIES,
  CACHE_TTL_MS,
  cache,
  cacheKey,
  clearCache,
  getCached,
  setCache,
} from './server.js';

test('cacheKey hashes the full normalized request payload', () => {
  const base = {
    title: 'chart',
    width: 800,
    height: 600,
    candles: [
      { time: '2026-04-01T00:00:00Z', open: 10, high: 12, low: 9, close: 11 },
      { time: '2026-04-01T01:00:00Z', open: 11, high: 13, low: 10, close: 12 },
    ],
  };
  const changed = {
    ...base,
    candles: [
      base.candles[0],
      { time: '2026-04-01T01:00:00Z', open: 11, high: 13, low: 10, close: 999 },
    ],
  };

  assert.notEqual(cacheKey(base), cacheKey(changed));
});

test('getCached removes expired entries on read', () => {
  clearCache();
  const key = 'expired';
  const png = Buffer.from('png');

  setCache(key, png, 0);
  assert.equal(getCached(key, CACHE_TTL_MS + 1), null);
  assert.equal(cache.has(key), false);
});

test('setCache prunes oldest entries when max size is exceeded', () => {
  clearCache();
  const now = 1000;

  for (let i = 0; i < CACHE_MAX_ENTRIES + 5; i += 1) {
    setCache(`key-${i}`, Buffer.from(String(i)), now + i);
  }

  assert.equal(cache.size, CACHE_MAX_ENTRIES);
  assert.equal(cache.has('key-0'), false);
  assert.equal(cache.has(`key-${CACHE_MAX_ENTRIES + 4}`), true);
});
