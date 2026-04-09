import { createServer } from 'node:http';
import { readFileSync, existsSync } from 'node:fs';
import { chromium } from 'playwright';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { createHash } from 'node:crypto';

const __dirname = dirname(fileURLToPath(import.meta.url));
const isMainModule = process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1];
const PORT = parseInt(process.env.PORT || '3100', 10);
const STATIC_PORT = parseInt(process.env.STATIC_PORT || '3101', 10);

// Load static files
const CHART_HTML = readFileSync(join(__dirname, 'chart.html'), 'utf-8');

// Find lightweight-charts standalone bundle
const LW_CANDIDATES = [
  join(__dirname, 'node_modules/lightweight-charts/dist/lightweight-charts.standalone.production.js'),
  join(__dirname, 'node_modules/lightweight-charts/dist/lightweight-charts.standalone.development.js'),
];
let LW_JS = '';
for (const p of LW_CANDIDATES) {
  if (existsSync(p)) { LW_JS = readFileSync(p, 'utf-8'); break; }
}
if (!LW_JS) {
  console.error('[renderer] lightweight-charts bundle not found!');
  process.exit(1);
}

// --- Static file server ---
const staticServer = createServer((req, res) => {
  if (req.url === '/lightweight-charts.standalone.production.js') {
    res.writeHead(200, { 'Content-Type': 'application/javascript', 'Cache-Control': 'public, max-age=86400' });
    res.end(LW_JS);
  } else {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end(CHART_HTML);
  }
});

// --- Cache ---
const cache = new Map();
const CACHE_TTL_MS = 5 * 60 * 1000;
const CACHE_MAX_ENTRIES = parseInt(process.env.CACHE_MAX_ENTRIES || '128', 10);

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    const keys = Object.keys(value).sort();
    return `{${keys.map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(',')}}`;
  }
  return JSON.stringify(value);
}

function cacheKey(body) {
  return createHash('sha256').update(stableStringify(body)).digest('hex');
}

function pruneCache(now = Date.now()) {
  for (const [key, entry] of cache.entries()) {
    if (now >= entry.expires) {
      cache.delete(key);
    }
  }
  while (cache.size > CACHE_MAX_ENTRIES) {
    const oldestKey = cache.keys().next().value;
    if (oldestKey === undefined) {
      break;
    }
    cache.delete(oldestKey);
  }
}

function getCached(key, now = Date.now()) {
  const e = cache.get(key);
  if (!e) return null;
  if (now >= e.expires) {
    cache.delete(key);
    return null;
  }
  cache.delete(key);
  cache.set(key, e);
  return e.png;
}

function setCache(key, png, now = Date.now()) {
  cache.delete(key);
  cache.set(key, { png, expires: now + CACHE_TTL_MS });
  pruneCache(now);
}

function clearCache() {
  cache.clear();
}

// --- Browser ---
let context = null;

async function ensureBrowser() {
  if (context) return;
  console.log('[renderer] launching browser...');
  context = await chromium.launchPersistentContext('/tmp/chart-renderer-profile', {
    headless: true,
    channel: 'chromium',
    deviceScaleFactor: 2,
    args: [
      '--no-sandbox',
      '--disable-setuid-sandbox',
      '--disable-dev-shm-usage',
      '--headless=new',
    ],
  });
  console.log('[renderer] browser ready');
}

async function renderChart(chartData, width, height) {
  await ensureBrowser();
  const page = await context.newPage();
  try {
    await page.setViewportSize({ width, height });

    // Collect console errors
    const errors = [];
    page.on('console', msg => { errors.push(`[${msg.type()}] ${msg.text()}`); });
    page.on('pageerror', err => errors.push('PAGE_ERROR: ' + err.message));

    await page.goto(`http://127.0.0.1:${STATIC_PORT}/`, { waitUntil: 'networkidle' });

    // Wait for lightweight-charts to load
    const lwReady = await page.evaluate(() => typeof LightweightCharts !== 'undefined' && typeof window.renderChart === 'function');
    if (!lwReady) {
      console.error('[renderer] LightweightCharts not loaded! errors:', errors);
      throw new Error('LightweightCharts not loaded');
    }

    try {
      await page.evaluate((data) => {
        window.renderChart(data);
        // Force a synchronous layout + paint by reading offsetHeight
        document.getElementById('chart').offsetHeight;
      }, chartData);
    } catch (evalErr) {
      console.error('[renderer] evaluate error:', evalErr.message, 'errors:', errors);
      throw evalErr;
    }
    if (errors.length > 0) {
      console.log('[renderer] browser console:', errors.join(' | '));
    }
    // Wait for canvas to actually paint (requestAnimationFrame + extra frame)
    await page.evaluate(() => new Promise(resolve => {
      requestAnimationFrame(() => requestAnimationFrame(resolve));
    }));
    await page.waitForTimeout(300);
    return await page.screenshot({ type: 'png' });
  } finally {
    await page.close();
  }
}

// --- HTTP API ---
function parseBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on('data', c => chunks.push(c));
    req.on('end', () => { try { resolve(JSON.parse(Buffer.concat(chunks))); } catch (e) { reject(e); } });
    req.on('error', reject);
  });
}

const server = createServer(async (req, res) => {
  if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ status: 'ok', cached: cache.size }));
    return;
  }

  if (req.method === 'POST' && req.url === '/render') {
    try {
      const body = await parseBody(req);
      const width = body.width || 800;
      const height = body.height || 600;

      const ck = cacheKey(body);
      const cached = getCached(ck);
      if (cached) {
        console.log(`[renderer] cache hit`);
        res.writeHead(200, { 'Content-Type': 'image/png', 'Content-Length': cached.length });
        res.end(cached);
        return;
      }

      const start = Date.now();
      const png = await renderChart(body, width, height);
      setCache(ck, png);
      console.log(`[renderer] rendered ${width}x${height} in ${Date.now() - start}ms (${png.length} bytes)`);

      res.writeHead(200, { 'Content-Type': 'image/png', 'Content-Length': png.length });
      res.end(png);
    } catch (err) {
      console.error('[renderer] error:', err.message);
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // Legacy endpoint (keep for backward compat)
  if (req.method === 'POST' && req.url === '/screenshot') {
    res.writeHead(410, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'Use POST /render with OHLC data instead' }));
    return;
  }

  res.writeHead(404);
  res.end('Not Found');
});

if (isMainModule) {
  staticServer.listen(STATIC_PORT, '127.0.0.1', () => {
    console.log(`[renderer] static server on :${STATIC_PORT}`);
  });

  ensureBrowser().then(() => {
    server.listen(PORT, () => console.log(`[renderer] listening on :${PORT}`));
  }).catch(err => {
    console.error('[renderer] failed:', err);
    process.exit(1);
  });

  process.on('SIGTERM', async () => {
    console.log('[renderer] shutting down...');
    if (context) await context.close();
    server.close();
    staticServer.close();
    process.exit(0);
  });
}

export { CACHE_MAX_ENTRIES, CACHE_TTL_MS, cache, cacheKey, clearCache, getCached, pruneCache, setCache, stableStringify };
