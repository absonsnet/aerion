// Bergamot offline translation in the webview (Phase D, Tier 2).
//
// The engine is Mozilla's Firefox Translations runtime (v0.6.0). It
// runs in a Web Worker: the loader defines a global loadBergamot(Module)
// and takes the WASM bytes directly, with no SharedArrayBuffer, no
// threads and no network/DOM access — so it runs in the Wails webview
// the same way the spellcheck worker already fetches its dictionaries.
//
// Nothing is bundled: the engine (WASM + loader, ~5 MB) and the
// language models are on-demand downloads, cached by the backend in
// the Daak data folder and served by the Go asset handler
// (/bergamot/engine/<file> and
// /bergamot/models/<from>-<to>/<version>/<file>). The engine downloads
// on first offline use — or from Settings → Translation — and stays
// cached until removed there; ensureWorker fetches it when it is not
// cached yet.
//
// One worker serves one route (one model, or the two models of a
// pivot through English), like Firefox, which runs one engine per
// language pair: a runtime that translated with one model and then
// loaded a second one crashed (memory access out of bounds).
// Workers are cached per route (at most two alive) and terminated
// when a third route is needed.
//
// Model files are fetched from the Go asset handler
// (/bergamot/models/<from>-<to>/<version>/<file>) — the backend
// downloads them on first need into the Daak data folder and lists
// them via ListBergamotModels. The engine files come from the same
// handler (/bergamot/engine/<file>), from the backend's on-disk cache
// under <data>/bergamot/runtime/.
import {
  ListBergamotModels,
  GetBergamotPairStatus,
  DownloadBergamotModel,
  GetBergamotEngineStatus,
  DownloadBergamotEngine,
} from '$lib/viewerBindings'
import type { translate } from '../../wailsjs/go/models'

// The worker bootstrap, appended to the glue source (bergamot-
// translator.js) in the worker blob. It is a plain string: no ${}
// interpolations, so the TS template literal below stays inert.
// Mirrors the molecule.dev bergamot worker (MPL-2.0, same runtime),
// adapted from Node worker_threads to a browser Worker.
const WORKER_BOOTSTRAP = `
var __bergamotReady = null;
var __ALIGNMENT = { model: 256, lex: 64, vocab: 64, srcvocab: 64, trgvocab: 64 };
var __MODEL_CONFIG = [
  'beam-size: 1',
  'normalize: 1.0',
  'word-penalty: 0',
  'max-length-break: 128',
  'mini-batch-words: 1024',
  'workspace: 128',
  'max-length-factor: 2.0',
  'skip-cost: true',
  'cpu-threads: 0',
  'quiet: true',
  'quiet-translation: true',
  'gemm-precision: int8shiftAlphaAll',
  'alignment: soft',
  ''
].join('\\n');

function __loadRuntime(wasmBinary) {
  return new Promise(function (resolve, reject) {
    try {
      var module = loadBergamot({
        INITIAL_MEMORY: 234291200,
        wasmBinary: wasmBinary,
        print: function () {},
        printErr: function () {},
        onAbort: function (reason) {
          reject(new Error('Bergamot runtime aborted: ' + String(reason)));
        },
        onRuntimeInitialized: function () {
          resolve(module);
        }
      });
    } catch (err) {
      reject(err);
    }
  });
}

function __loadModel(bergamot, spec) {
  function memory(role, bytes) {
    var aligned = new bergamot.AlignedMemory(bytes.byteLength, __ALIGNMENT[role]);
    // bytes arrives as an ArrayBuffer (postMessage transfer). A raw
    // ArrayBuffer is NOT a TypedArray, and TypedArray.set() silently
    // copies nothing from one — the model would load as all zeros and
    // Marian aborts parsing it. Wrap it: new Uint8Array views the
    // same buffer, and set() copies from a view. (In Node the
    // identical line works because readFileSync returns a Buffer,
    // which IS a Uint8Array.)
    aligned.getByteArrayView().set(new Uint8Array(bytes));
    return aligned;
  }
  var vocabs = new bergamot.AlignedMemoryList();
  if (spec.files.vocab) {
    vocabs.push_back(memory('vocab', spec.files.vocab));
  } else if (spec.files.srcvocab && spec.files.trgvocab) {
    vocabs.push_back(memory('srcvocab', spec.files.srcvocab));
    vocabs.push_back(memory('trgvocab', spec.files.trgvocab));
  } else {
    throw new Error('Bergamot model has no vocabulary');
  }
  return new bergamot.TranslationModel(
    spec.from, spec.to, __MODEL_CONFIG,
    memory('model', spec.files.model),
    memory('lex', spec.files.lex),
    vocabs, null
  );
}

function __start(init) {
  return __loadRuntime(init.wasm).then(function (bergamot) {
    var service = new bergamot.BlockingService({ cacheSize: 0 });
    var models = init.models.map(function (spec) {
      return __loadModel(bergamot, spec);
    });
    return { bergamot: bergamot, service: service, models: models };
  });
}

function __translate(ctx, input) {
  var bergamot = ctx.bergamot;
  var service = ctx.service;
  var models = ctx.models;
  var out = input.slice();
  var positions = [];
  var edges = [];
  var texts = new bergamot.VectorString();
  var options = new bergamot.VectorResponseOptions();
  var responses = null;
  try {
    input.forEach(function (text, i) {
      var match = /^(\\s*)([\\s\\S]*?)(\\s*)$/.exec(text);
      if (!match || match[2] === '') return;
      positions.push(i);
      edges.push([match[1], match[3]]);
      texts.push_back(match[2]);
      options.push_back({ qualityScores: false, alignment: false, html: false });
    });
    if (positions.length === 0) return out;
    responses = models.length === 1
      ? service.translate(models[0], texts, options)
      : service.translateViaPivoting(models[0], models[1], texts, options);
    positions.forEach(function (position, k) {
      out[position] = edges[k][0] + responses.get(k).getTranslatedText() + edges[k][1];
    });
    return out;
  } finally {
    texts.delete();
    options.delete();
    if (responses) responses.delete();
  }
}

self.onmessage = function (e) {
  var msg = e.data;
  if (msg.type === 'init') {
    __bergamotReady = __start(msg);
    __bergamotReady.then(
      function () { self.postMessage({ type: 'ready' }); },
      function (err) { self.postMessage({ type: 'error', error: String(err && err.message || err) }); }
    );
    return;
  }
  if (msg.type === 'translate') {
    __bergamotReady.then(function (ctx) {
      try {
        var texts = __translate(ctx, msg.texts);
        self.postMessage({ type: 'result', id: msg.id, texts: texts });
      } catch (err) {
        self.postMessage({ type: 'error', id: msg.id, error: String(err && err.message || err) });
      }
    }, function (err) {
      self.postMessage({ type: 'error', id: msg.id, error: String(err && err.message || err) });
    });
    return;
  }
};
`

// One loaded route: a worker plus the request plumbing.
interface RouteWorker {
  key: string
  worker: Worker
  ready: Promise<void>
  nextId: number
  pending: Map<number, { resolve: (texts: string[]) => void; reject: (err: Error) => void }>
}

const routeWorkers = new Map<string, RouteWorker>()
const MAX_WORKERS = 2

let glueText: string | null = null
let wasmBytes: ArrayBuffer | null = null

async function loadGlue(): Promise<string> {
  if (glueText !== null) return glueText
  const res = await fetch('/bergamot/engine/bergamot-translator.js')
  if (!res.ok) throw new Error('Could not load the Bergamot engine (HTTP ' + res.status + ')')
  glueText = await res.text()
  return glueText
}

async function loadWasm(): Promise<ArrayBuffer> {
  if (wasmBytes !== null) return wasmBytes.slice(0)
  const res = await fetch('/bergamot/engine/bergamot-translator.wasm')
  if (!res.ok) throw new Error('Could not load the Bergamot engine (HTTP ' + res.status + ')')
  wasmBytes = await res.arrayBuffer()
  return wasmBytes.slice(0)
}

// ensureBergamotEngine downloads the engine (WASM + loader, ~5 MB)
// when it is not cached yet, so a worker can start. The backend
// verifies both files against Mozilla's published hashes.
async function ensureBergamotEngine(): Promise<void> {
  const status = await GetBergamotEngineStatus()
  if (status.available) return
  await DownloadBergamotEngine()
  // The cached bytes are stale once the engine is (re-)downloaded.
  glueText = null
  wasmBytes = null
}

async function fetchModelFile(
  from: string,
  to: string,
  version: string,
  file: string
): Promise<ArrayBuffer> {
  const res = await fetch(
    '/bergamot/models/' + from + '-' + to + '/' + version + '/' + encodeURIComponent(file)
  )
  if (!res.ok) {
    throw new Error('Could not load the Bergamot model file ' + file + ' (HTTP ' + res.status + ')')
  }
  return res.arrayBuffer()
}

type PairFiles = { from: string; to: string; version: string; files: Record<string, string> }

// routeLegs returns the downloaded pairs for a route (direct, or a
// pivot through English), or throws naming the missing leg.
async function routeLegs(
  from: string,
  to: string
): Promise<PairFiles[]> {
  const legs: Array<[string, string]> =
    from === 'en' || to === 'en' ? [[from, to]] : [[from, 'en'], ['en', to]]
  const result = await ListBergamotModels()
  const downloaded = result.pairs as unknown as Array<translate.DownloadedPair>
  const out: PairFiles[] = []
  for (const [lf, lt] of legs) {
    const pair = downloaded.find((p) => p.fromLang === lf && p.toLang === lt)
    if (!pair || !pair.files?.model || !pair.files?.lex || !pair.files?.vocab) {
      const status = await GetBergamotPairStatus(lf, lt)
      const leg = (status as unknown as Array<{ supported: boolean; sizeBytes: number }>)[0]
      const err = new Error(
        leg && !leg.supported
          ? 'No offline model exists for ' + lf + ' to ' + lt
          : 'The offline model for ' + lf + ' to ' + lt + ' is not downloaded yet'
      ) as Error & { bergamotMissing?: { from: string; to: string; sizeBytes: number } }
      err.bergamotMissing = { from: lf, to: lt, sizeBytes: leg?.sizeBytes ?? 0 }
      throw err
    }
    out.push({ from: lf, to: lt, version: pair.version, files: pair.files })
  }
  return out
}

// ensureWorker returns a ready worker for a route, starting one when
// needed (and dropping the least recently used worker past the cap).
async function ensureWorker(from: string, to: string): Promise<RouteWorker> {
  const key = from + '>' + to
  const existing = routeWorkers.get(key)
  if (existing) {
    await existing.ready
    // Touch for LRU order.
    routeWorkers.delete(key)
    routeWorkers.set(key, existing)
    return existing
  }
  const legs = await routeLegs(from, to)
  await ensureBergamotEngine()
  const [glue, wasm] = await Promise.all([loadGlue(), loadWasm()])
  const models: Array<{
    from: string
    to: string
    files: { model: ArrayBuffer; lex: ArrayBuffer; vocab: ArrayBuffer }
  }> = []
  const transfer: ArrayBuffer[] = [wasm]
  for (const leg of legs) {
    const [model, lex, vocab] = await Promise.all([
      fetchModelFile(leg.from, leg.to, leg.version, leg.files.model),
      fetchModelFile(leg.from, leg.to, leg.version, leg.files.lex),
      fetchModelFile(leg.from, leg.to, leg.version, leg.files.vocab),
    ])
    transfer.push(model, lex, vocab)
    models.push({ from: leg.from, to: leg.to, files: { model, lex, vocab } })
  }

  const blob = new Blob([glue + '\n;' + WORKER_BOOTSTRAP], { type: 'text/javascript' })
  const worker = new Worker(URL.createObjectURL(blob))
  const route: RouteWorker = { key, worker, ready: Promise.resolve(), nextId: 1, pending: new Map() }
  route.ready = new Promise<void>((resolve, reject) => {
    const onMessage = (e: MessageEvent) => {
      const msg = e.data as { type: string; id?: number; texts?: string[]; error?: string }
      if (msg.type === 'ready' && msg.id === undefined) {
        resolve()
        return
      }
      if (msg.type === 'error' && msg.id === undefined) {
        worker.removeEventListener('message', onMessage)
        reject(new Error(msg.error ?? 'Bergamot failed to start'))
        return
      }
      if (msg.type === 'result' && msg.id !== undefined) {
        const p = route.pending.get(msg.id)
        if (p) {
          route.pending.delete(msg.id)
          p.resolve(msg.texts ?? [])
        }
        return
      }
      if (msg.type === 'error' && msg.id !== undefined) {
        const p = route.pending.get(msg.id)
        if (p) {
          route.pending.delete(msg.id)
          p.reject(new Error(msg.error ?? 'Bergamot translation failed'))
        }
      }
    }
    worker.addEventListener('message', onMessage)
    worker.addEventListener('error', () => reject(new Error('Bergamot worker failed to start')))
  })
  worker.postMessage({ type: 'init', wasm, models }, transfer)
  await route.ready

  routeWorkers.set(key, route)
  while (routeWorkers.size > MAX_WORKERS) {
    const oldest = routeWorkers.keys().next().value
    if (oldest === undefined || oldest === key) break
    disposeRoute(oldest)
  }
  return route
}

function disposeRoute(key: string) {
  const route = routeWorkers.get(key)
  if (!route) return
  routeWorkers.delete(key)
  for (const p of route.pending.values()) p.reject(new Error('Bergamot worker was closed'))
  route.pending.clear()
  route.worker.terminate()
}

/** Drops all Bergamot workers (e.g. after a model is deleted). */
export function disposeBergamot() {
  for (const key of [...routeWorkers.keys()]) disposeRoute(key)
  glueText = null
  wasmBytes = null
}

/** True when this webview can run a worker at all. */
export function isBergamotAvailable(): boolean {
  return typeof Worker !== 'undefined'
}

/**
 * Translates plain-text segments offline with Bergamot. Segments are
 * translated in order; blank segments pass through unchanged. Throws
 * an error with a `bergamotMissing` property ({from, to, sizeBytes})
 * when a model is not downloaded yet, so the caller can offer the
 * download.
 */
export async function translateWithBergamot(
  texts: string[],
  from: string,
  to: string
): Promise<string[]> {
  if (!isBergamotAvailable()) throw new Error('This device cannot run the offline translation engine')
  const route = await ensureWorker(from, to)
  const id = route.nextId++
  const out = await new Promise<string[]>((resolve, reject) => {
    route.pending.set(id, { resolve, reject })
    route.worker.postMessage({ type: 'translate', id, texts })
  })
  return out
}

/**
 * Translates an HTML body offline with Bergamot: only text nodes go
 * through the engine, so paragraphs, lists, links (URLs unchanged,
 * link text translated), tables and buttons keep their structure.
 * Images, attachments and quoted signatures are untouched in the
 * sense that their markup is never rewritten — the quoted-history
 * block is translated too but stays visually quoted. onProgress
 * reports translated/total text segments; onPartialHtml receives the
 * current translated HTML after each batch, so long messages render
 * progressively instead of only at the end.
 */
export async function translateHtmlWithBergamot(
  html: string,
  from: string,
  to: string,
  onProgress?: (done: number, total: number) => void,
  onPartialHtml?: (html: string) => void
): Promise<string> {
  const { doc, nodes } = collectTextNodes(html)
  if (nodes.length === 0) return html

  const BATCH = 32
  let done = 0
  for (let i = 0; i < nodes.length; i += BATCH) {
    const batch = nodes.slice(i, i + BATCH)
    const translated = await translateWithBergamot(
      batch.map((n) => n.textContent ?? ''),
      from,
      to
    )
    batch.forEach((n, k) => {
      n.textContent = translated[k] ?? n.textContent
    })
    done += batch.length
    onProgress?.(done, nodes.length)
    onPartialHtml?.(doc.body.innerHTML)
  }
  return doc.body.innerHTML
}

/**
 * Splits an HTML body into its translatable text nodes, in document
 * order. Script/style/noscript/template content and blank nodes are
 * skipped. The returned doc owns the nodes: translating them in place
 * and reading doc.body.innerHTML gives the translated HTML with the
 * original markup intact (links keep their hrefs — only text nodes
 * are ever touched). Shared with the online path, which sends the
 * same segments to the provider instead of the engine.
 */
export function collectTextNodes(html: string): { doc: Document; nodes: Text[] } {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const parent = node.parentElement
      if (!parent) return NodeFilter.FILTER_REJECT
      const tag = parent.tagName
      if (tag === 'SCRIPT' || tag === 'STYLE' || tag === 'NOSCRIPT' || tag === 'TEMPLATE') {
        return NodeFilter.FILTER_REJECT
      }
      if (node.textContent === null || node.textContent.trim() === '') {
        return NodeFilter.FILTER_REJECT
      }
      return NodeFilter.FILTER_ACCEPT
    },
  })
  const nodes: Text[] = []
  let node: Text | null
  while ((node = walker.nextNode() as Text | null)) nodes.push(node)
  return { doc, nodes }
}

/** Downloads a Bergamot pair (all legs of the route) via the backend. */
export async function downloadBergamotModel(from: string, to: string): Promise<void> {
  await DownloadBergamotModel(from, to)
}

/** Downloads the Bergamot engine (WASM + loader, ~5 MB) via the backend. */
export async function downloadBergamotEngine(): Promise<void> {
  await DownloadBergamotEngine()
}
