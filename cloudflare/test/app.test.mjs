import test from 'node:test';
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {readFileSync} from 'node:fs';
import {handle} from '../src/app.mjs';
import {sections, findAll, markHTML, parseTerms, queryMinimum, literalMatch} from '../src/text.mjs';
import {embed, meaningSearch, writeVectors, modelRequest} from '../src/services.mjs';

class D1 {
  constructor() { this.db = new DatabaseSync(':memory:'); this.db.exec(readFileSync(new URL('../schema.sql', import.meta.url), 'utf8')); }
  prepare(sql) {
    const db = this.db;
    let values = [];
    const statement = {
      bind(...params) { values = params; return statement; },
      async first() { return db.prepare(sql).get(...values) || null; },
      async all() { return {success: true, results: db.prepare(sql).all(...values)}; },
      async run() { const result = db.prepare(sql).run(...values); return {success: true, meta: result}; },
      sql
    };
    return statement;
  }
  async batch(statements) {
    this.db.exec('BEGIN');
    try {
      const result = [];
      for (const statement of statements) result.push(/^\s*SELECT/i.test(statement.sql) ? await statement.all() : await statement.run());
      this.db.exec('COMMIT');
      return result;
    } catch (error) { this.db.exec('ROLLBACK'); throw error; }
  }
}

const config = {term: {}, embed: {model: 'm'}, astra: {apiEndpoint: 'https://example.com', applicationToken: 'test-token', keyspace: 'default_keyspace', collection: 'vectors'}, similarityMin: 0.55, shortQuerySimilarityMin: 0.60};

function environment() { return {DB: new D1(), APP_CONFIG: JSON.stringify(config), DERIVE_WORKFLOW: {async create() {}}}; }
function request(path, body, origin = 'https://prompts.example.com') {
  return new Request('https://prompts.example.com' + path, body === undefined ? {} : {method: 'POST', headers: {Origin: origin, 'Content-Type': 'application/x-www-form-urlencoded'}, body: new URLSearchParams(body)});
}

async function withFetch(handler, work) {
  const original = globalThis.fetch;
  globalThis.fetch = handler;
  try { await work(); } finally { globalThis.fetch = original; }
}

function response(data) { return Response.json(data); }

test('original UTF-8 offsets and escaped highlights survive cloud migration', () => {
  const body = '场景：\r\n\r\n海滩与😀 <script>\r\n\r\n另一段';
  const split = sections(body);
  assert.equal(split.length, 2);
  assert.equal(split[0].text, '场景：\r\n\r\n海滩与😀 <script>\r');
  const marked = markHTML(body, findAll(body, '海滩'));
  assert.match(marked, /<mark>海滩<\/mark>/);
  assert.ok(!marked.includes('<script>'));
  const terms = parseTerms(body, '{"keywords":[{"phrase":"海滩"},{"phrase":"invented"}],"temperament":[{"kind":"style","phrase":"另一段","spellings":["other"]}]}');
  assert.equal(terms.length, 2);
  assert.ok(literalMatch({body}, terms, 'other'));
  assert.equal(queryMinimum(' 美女 ', config), 0.60);
  assert.equal(queryMinimum('海滩上的成年女性', config), 0.55);
  assert.equal(queryMinimum('美女', {...config, similarityMin: 0.75}), 0.75);
});

test('cloud forms preserve text, validate URLs, and reject cross-origin writes', async () => {
  const env = environment();
  const body = '原文😀\r\n<script>alert(1)</script>';
  const created = await handle(request('/prompts', {body, example_url: 'https://example.com'}), env);
  assert.equal(created.status, 303);
  const path = created.headers.get('Location');
  const detail = await handle(request(path), env);
  const html = await detail.text();
  assert.ok(html.includes('&lt;script&gt;'));
  const stored = await env.DB.prepare('SELECT * FROM prompts').first();
  assert.equal(stored.body, body);
  const generation = stored.derive_generation;
  await handle(request(path, {body, example_url: 'https://example.com/new'}), env);
  assert.equal((await env.DB.prepare('SELECT * FROM prompts').first()).derive_generation, generation);
  assert.equal((await handle(request(path, {body: 'attack'}, 'https://elsewhere.example'), env)).status, 403);
  const invalid = await handle(request('/prompts', {body: 'text', example_url: 'javascript:alert(1)'}), env);
  assert.ok((await invalid.text()).includes('示例链接必须'));
  assert.equal((await env.DB.prepare('SELECT COUNT(*) AS n FROM prompts').first()).n, 1);
  env.DB.db.close();
});

test('Astra short-word scores filter game, long queries keep threshold and exhausted results do not refetch', async () => {
  let searches = 0;
  const prompts = [{id: 'beach', body: '成年女性', derive_generation: 0}, {id: 'game', body: 'ニアミス', derive_generation: 0}];
  await withFetch(async () => {
    searches++;
    return response({data: {documents: [{promptId: 'beach', generation: 0, revision: 'beach:0', start: 0, end: 12, $similarity: 0.8324376}, {promptId: 'game', generation: 0, revision: 'game:0', start: 0, end: 12, $similarity: 0.78519416}]}});
  }, async () => {
    const short = await meaningSearch(config, prompts, [1, 0], queryMinimum('美女', config), 5);
    assert.deepEqual(short.map(hit => hit.prompt.id), ['beach']);
    const long = await meaningSearch(config, prompts, [1, 0], queryMinimum('请帮我查找美女', config), 5);
    assert.deepEqual(long.map(hit => hit.prompt.id), ['beach', 'game']);
    assert.equal(searches, 2);
  });
});

test('workflow vector batches keep absolute indexes and use idempotent upserts', async () => {
  const ids = [];
  await withFetch(async (_url, options) => {
    const command = JSON.parse(options.body);
    if (command.findOneAndReplace) { ids.push(command.findOneAndReplace.replacement._id); assert.equal(command.findOneAndReplace.options.upsert, true); }
    return response({status: {ok: 1}});
  }, async () => {
    await writeVectors(config, {id: 'p', derive_generation: 2}, [{source: 'term', start: 0, end: 3, index: 8, values: [1, 0]}, {source: 'term', start: 0, end: 3, index: 9, values: [1, 0]}]);
    assert.deepEqual(ids, ['p:2:8', 'p:2:9']);
  });
});

test('embeddings reject duplicated indexes instead of assigning vectors to the wrong text', async () => {
  await withFetch(async () => response({data: [{index: 0, embedding: [1, 0]}, {index: 0, embedding: [0, 1]}]}), async () => {
    await assert.rejects(embed({...config, embed: {apiKey: 'test', model: 'm', baseUrl: 'https://example.com'}}, ['a', 'b']), /嵌入响应格式错误/);
  });
});


test('model redirects fail without forwarding provider credentials', async () => {
  let calls = 0;
  await withFetch(async (url, options) => {
    calls++;
    assert.equal(options.redirect, 'manual');
    return new Response(null, {status: 302, headers: {Location: 'https://elsewhere.example.com'}});
  }, async () => {
    await assert.rejects(modelRequest({baseUrl: 'https://provider.example.com', apiKey: 'test-token', model: 'm'}, '/embeddings', {input: ['text']}), /HTTP 302/);
  });
  assert.equal(calls, 1);
});
