import test from 'node:test';
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {readFileSync} from 'node:fs';
import {handle} from '../src/app.mjs';
import {sections, findAll, markHTML, parseTerms, queryMinimum, literalMatch, addedOn, exampleLabel} from '../src/text.mjs';
import {embed, meaningSearch, writeVectors, modelRequest, configuration, redact, writingExamples, keepAstraAwake} from '../src/services.mjs';

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

test('hourly keepalive writes one small vector and then deletes it', async () => {
  const calls = [];
  await withFetch(async (url, options) => {
    const command = JSON.parse(options.body);
    const name = Object.keys(command)[0];
    calls.push({name, url: String(url), command});
    assert.equal(options.headers.Token, 'test-token');
    if (name === 'findCollections') return response({status: {collections: [{name: 'prompt_vectors_cloudflare', options: {vector: {dimension: 4096, metric: 'cosine'}}}]}});
    return response({status: {deletedCount: name === 'deleteOne' ? 0 : 1}});
  }, async () => { await keepAstraAwake(config); });
  assert.deepEqual(calls.map(call => call.name), ['findCollections', 'createCollection', 'deleteOne', 'insertOne', 'deleteOne']);
  assert.equal(calls[1].url.endsWith('/api/json/v1/default_keyspace'), true);
  assert.deepEqual(calls[1].command.createCollection, {name: 'keepalive', options: {vector: {dimension: 2, metric: 'cosine'}}});
  assert.equal(calls[3].url.endsWith('/keepalive'), true);
  assert.deepEqual(calls[3].command.insertOne.document, {_id: '__keepalive__', $vector: [1, 0]});
  assert.deepEqual(calls[2].command.deleteOne.filter, {_id: '__keepalive__'});
  assert.deepEqual(calls[4].command.deleteOne.filter, {_id: '__keepalive__'});
});

test('keepalive skips an unconfigured database and an existing collection', async () => {
  let calls = 0;
  await withFetch(async () => { calls++; return response({}); }, async () => { await keepAstraAwake({astra: {}}); });
  assert.equal(calls, 0);
  const names = [];
  await withFetch(async (_url, options) => {
    const command = JSON.parse(options.body);
    names.push(Object.keys(command)[0]);
    return response({status: {collections: [{name: 'keepalive', options: {vector: {dimension: 2, metric: 'cosine'}}}], deletedCount: 1}});
  }, async () => { await keepAstraAwake(config); });
  assert.deepEqual(names, ['findCollections', 'deleteOne', 'insertOne', 'deleteOne']);
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

test('draft config keeps the default base URL and redacts its key', () => {
  const parsed = configuration({APP_CONFIG: JSON.stringify({term: {apiKey: 'term-key'}})});
  assert.equal(parsed.draft.baseUrl, 'https://openrouter.ai/api/v1');
  assert.equal(parsed.draft.apiKey, undefined);
  const secret = 'draft-secret-key';
  assert.equal(redact(new Error(`failed ${secret}`), {...parsed, draft: {apiKey: secret}}).includes(secret), false);
  assert.equal(addedOn('2026-09-28T16:00:00.000Z'), '添加于 2026-09-29');
  assert.equal(addedOn('2026-09-28T15:59:59.000Z'), '添加于 2026-09-28');
});

function draftEnvironment() {
  const env = environment();
  env.APP_CONFIG = JSON.stringify({
    ...config,
    embed: {apiKey: 'embed-key', model: 'm', baseUrl: 'https://embed.example'},
    draft: {apiKey: 'draft-secret-key', model: 'vendor/draft', baseUrl: 'https://draft.example'},
    similarityMin: 0.35,
    shortQuerySimilarityMin: 0.60
  });
  return env;
}

async function insertPrompt(env, prompt) {
  await env.DB.prepare('INSERT INTO prompts (id, body, body_search, example_url, created_at, updated_at, derive_generation, derive_status) VALUES (?, ?, ?, ?, ?, ?, 0, ?)').bind(prompt.id, prompt.body, prompt.body.toLowerCase(), prompt.example_url || null, prompt.created_at, prompt.created_at, prompt.derive_status || 'ready').run();
}

function vectorDocument(prompt, similarity) {
  return {promptId: prompt.id, generation: 0, revision: `${prompt.id}:0`, start: 0, end: Buffer.byteLength(prompt.body), $similarity: similarity};
}

test('writing examples ignore literal exclusion and the short-query minimum', async () => {
  const env = draftEnvironment();
  const prompts = [
    {id: 'near', body: '第一全文'},
    {id: 'mid', body: '第二全文'},
    {id: 'low', body: '第三全文'},
    {id: 'out', body: '第四全文'},
    {id: 'extra', body: '第五全文'},
    {id: 'literal', body: '热带海滩', example_url: 'https://example.com/do-not-send'},
    {id: 'short', body: '短需求范例'},
    {id: 'idle', body: '没有向量', derive_status: 'none'}
  ];
  for (const prompt of prompts) await insertPrompt(env, {...prompt, created_at: '2026-09-28T16:00:00.000Z'});
  const documents = {
    '无关的需求': [vectorDocument(prompts[0], 0.95), vectorDocument(prompts[1], 0.90), vectorDocument(prompts[2], 0.80), vectorDocument(prompts[3], 0.60)],
    '五条都过线': [vectorDocument(prompts[0], 0.99), vectorDocument(prompts[1], 0.95), vectorDocument(prompts[2], 0.90), vectorDocument(prompts[3], 0.85), vectorDocument(prompts[4], 0.80)],
    '海滩': [vectorDocument(prompts[5], 0.95)],
    '剪纸': [vectorDocument(prompts[6], 0.70)]
  };
  let filter;
  let embeddedNeed = '';
  await withFetch(async (url, options) => {
    const raw = String(options.body || '');
    if (String(url).includes('/embeddings')) {
      if (raw.includes('嵌入失败')) return new Response('down', {status: 500});
      embeddedNeed = JSON.parse(raw).input[0];
      return response({data: [{index: 0, embedding: [1, 0]}]});
    }
    const command = JSON.parse(raw);
    filter = command.find.filter;
    return response({data: {documents: documents[embeddedNeed] || []}});
  }, async () => {
    const parsed = configuration(env);
    const top = await writingExamples(env, parsed, '无关的需求');
    assert.deepEqual(top.map(prompt => prompt.id), ['near', 'mid', 'low']);
    assert.equal(filter.revision.$in.includes('idle:0'), false);
    const capped = await writingExamples(env, parsed, '五条都过线');
    assert.deepEqual(capped.map(prompt => prompt.id), ['near', 'mid', 'low']);
    const literal = await writingExamples(env, parsed, '海滩');
    assert.equal(literal[0].id, 'literal');
    assert.equal(literal[0].body, '热带海滩');
    assert.equal(filter.revision.$in.includes('literal:0'), true);
    const short = await writingExamples(env, parsed, '剪纸');
    assert.equal(short[0].id, 'short');
    assert.deepEqual(await writingExamples(env, parsed, '嵌入失败'), []);
  });
  env.DB.db.close();
});

test('pages copy the stored prompt and show the Beijing date', async () => {
  const env = draftEnvironment();
  const late = 'late';
  const early = 'early';
  const meaning = 'meaning';
  const lateBody = `${'海'.repeat(130)}海滩<script>alert(1)</script></textarea>`;
  await insertPrompt(env, {id: late, body: lateBody, example_url: 'https://example.com/keep-out', created_at: '2026-09-28T16:00:00.000Z'});
  await insertPrompt(env, {id: early, body: '较早的提示词', created_at: '2026-09-28T15:59:59.000Z'});
  await insertPrompt(env, {id: meaning, body: 'A neon-lit street', created_at: '2026-09-28T12:00:00.000Z'});
  await withFetch(async (url, options) => {
    if (String(url).includes('/embeddings')) return response({data: [{index: 0, embedding: [1, 0]}]});
    const command = JSON.parse(options.body);
    if (command.find) return response({data: {documents: [vectorDocument({id: meaning, body: 'A neon-lit street'}, 0.95)]}});
    return response({});
  }, async () => {
    const script = await handle(request('/copy.js'), env);
    const source = await script.text();
    assert.equal(script.headers.get('Content-Type'), 'text/javascript; charset=utf-8');
    assert.equal(source.includes('innerHTML'), false);
    assert.equal(source.includes('navigator.clipboard.writeText'), true);
    const list = await handle(request('/'), env);
    const html = await list.text();
    assert.equal(list.headers.get('Content-Security-Policy').includes("script-src 'self'"), true);
    assert.equal(list.headers.get('Content-Security-Policy').includes("default-src 'none'"), true);
    assert.equal(html.includes('添加于 2026-09-29'), true);
    assert.equal(html.includes('添加于 2026-09-28'), true);
    assert.equal(html.includes('写一条'), true);
    assert.equal(html.includes('class="count">3 条'), true);
    assert.equal(html.includes('placeholder="原文、一个词，或一句意思"'), true);
    assert.equal(html.includes('>example.com/keep-out</a>'), true);
    assert.equal(html.includes('href="https://example.com/keep-out"'), true);
    assert.equal(exampleLabel('https://x.com/riku720720/status/2102515055116063144'), 'x.com/riku720720');
    assert.equal(exampleLabel('https://twitter.com/ohmuyi/status/1?s=20'), 'x.com/ohmuyi');
    const field = html.slice(html.indexOf(`<textarea id="copy-${late}" hidden readonly>`), html.indexOf('</textarea>', html.indexOf(`copy-${late}`)));
    assert.equal(field.includes('海'.repeat(130)), true);
    assert.equal(field.includes('&lt;script&gt;alert(1)&lt;/script&gt;&lt;/textarea&gt;'), true);
    assert.equal(field.includes('keep-out'), false);
    assert.equal(html.replaceAll('<script src="/copy.js"></script>', '').toLowerCase().includes('<script'), false);
    const detail = await (await handle(request(`/prompts/${late}`), env)).text();
    assert.equal(detail.includes('添加于'), false);
    assert.equal(detail.includes('&lt;script&gt;'), true);
    const found = await (await handle(request('/?q=' + encodeURIComponent('海滩')), env)).text();
    assert.equal(found.includes('意思相近'), true);
    assert.equal(found.includes(`copy-${late}`), true);
    assert.equal(found.includes(`copy-${meaning}`), true);
    assert.equal(found.includes('添加于 2026-09-28'), true);
    const corrected = await handle(request(`/prompts/${late}`, {body: '改正后的海滩全文', example_url: 'https://example.com/keep-out'}), env);
    assert.equal(corrected.status, 303);
    const after = await (await handle(request('/'), env)).text();
    assert.equal(after.includes('添加于 2026-09-29'), true);
    assert.equal(after.includes('改正后的海滩全文'), true);
  });
  env.DB.db.close();
});

test('draft opens in the paste form and stays unsaved until submit', async () => {
  const env = draftEnvironment();
  await insertPrompt(env, {id: 'sample', body: '喜欢剪纸风格', example_url: 'https://example.com/do-not-send', created_at: '2026-09-28T16:00:00.000Z'});
  const chats = [];
  await withFetch(async (url, options) => {
    const target = String(url);
    if (target.includes('/embeddings')) {
      if (options.body.includes('嵌入失败')) return new Response('down', {status: 500});
      return response({data: [{index: 0, embedding: [1, 0]}]});
    }
    if (target.includes('/chat/completions')) {
      chats.push(options.body);
      const payload = JSON.parse(options.body);
      const user = payload.messages.find(message => message.role === 'user').content;
      if (user.includes('空白稿')) return response({choices: [{message: {content: '   '}}]});
      if (user.includes('泄露密钥')) return new Response('draft-secret-key leaked', {status: 500});
      return response({choices: [{message: {content: '生成的正文\n第二行'}}]});
    }
    const command = JSON.parse(options.body);
    if (command.find) return response({data: {documents: [vectorDocument({id: 'sample', body: '喜欢剪纸风格'}, 0.70)]}});
    return response({});
  }, async () => {
    const opened = await (await handle(request('/prompts/draft'), env)).text();
    assert.equal(opened.includes('要写的内容'), true);
    assert.equal(opened.includes('未找到'), false);
    const empty = await handle(request('/prompts/draft', {need: '  '}), env);
    assert.equal((await empty.text()).includes('要写的内容必填'), true);
    assert.equal(chats.length, 0);
    const bare = environment();
    const unconfigured = await (await handle(request('/prompts/draft', {need: '春节厨房'}), bare)).text();
    assert.equal(unconfigured.includes('还不能生成'), true);
    assert.equal(chats.length, 0);
    bare.DB.db.close();
    const before = (await env.DB.prepare('SELECT COUNT(*) AS n FROM prompts').first()).n;
    const drafted = await handle(request('/prompts/draft', {need: '剪纸'}), env);
    const draftHTML = await drafted.text();
    assert.equal(draftHTML.includes('生成的正文\n第二行'), true);
    assert.equal(draftHTML.includes('这是生成的草稿，改完再保存'), true);
    assert.equal(draftHTML.includes('这次没有参考'), false);
    assert.equal(draftHTML.includes('action="/prompts"'), true);
    assert.equal(draftHTML.includes('name="example_url" value=""'), true);
    const requestBody = chats.at(-1);
    assert.equal(requestBody.includes('要写的内容'), true);
    assert.equal(requestBody.includes('写法范例'), true);
    assert.equal(requestBody.includes('喜欢剪纸风格'), true);
    assert.equal(requestBody.includes('do-not-send'), false);
    assert.equal((await env.DB.prepare('SELECT COUNT(*) AS n FROM prompts').first()).n, before);
    const failedEmbed = await (await handle(request('/prompts/draft', {need: '嵌入失败也要写'}), env)).text();
    assert.equal(failedEmbed.includes('这次没有参考'), true);
    assert.equal(failedEmbed.includes('生成的正文'), true);
    assert.equal((await env.DB.prepare('SELECT COUNT(*) AS n FROM prompts').first()).n, before);
    const blank = await (await handle(request('/prompts/draft', {need: '空白稿'}), env)).text();
    assert.equal(blank.includes('没有生成出来'), true);
    assert.equal(blank.includes('空白稿'), true);
    assert.equal(blank.includes('这是生成的草稿'), false);
    const leaked = await (await handle(request('/prompts/draft', {need: '泄露密钥'}), env)).text();
    assert.equal(leaked.includes('没有生成出来'), true);
    assert.equal(leaked.includes('draft-secret-key'), false);
    const saved = await handle(request('/prompts', {body: '改过的正文'}), env);
    assert.equal(saved.status, 303);
    const stored = await env.DB.prepare('SELECT body FROM prompts WHERE body = ?').bind('改过的正文').first();
    assert.equal(stored.body, '改过的正文');
    assert.equal(await env.DB.prepare('SELECT body FROM prompts WHERE body = ?').bind('剪纸').first(), null);
  });
  env.DB.db.close();
});
