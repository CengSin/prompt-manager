import {byteSlice, literalMatch, queryMinimum} from './text.mjs';

export function configuration(env) {
  const config = JSON.parse(env.APP_CONFIG || '{}');
  for (const name of ['term', 'embed']) config[name] = {baseUrl: 'https://openrouter.ai/api/v1', ...config[name]};
  config.astra = {keyspace: 'default_keyspace', collection: 'prompt_vectors_cloudflare', ...config.astra};
  return config;
}

export function redact(error, config) {
  let text = String(error?.message || error);
  for (const secret of [config.term?.apiKey, config.embed?.apiKey, config.astra?.applicationToken]) if (secret) text = text.replaceAll(secret, '[redacted]');
  return text.slice(0, 500);
}

export async function responseJSON(response, maximum = 20 * 1024 * 1024) {
  if (!response.ok) { await response.body?.cancel(); throw new Error(`接口返回 HTTP ${response.status}`); }
  const reader = response.body.getReader();
  const chunks = [];
  let length = 0;
  try {
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      length += value.length;
      if (length > maximum) throw new Error('接口响应超过大小上限');
      chunks.push(value);
    }
  } finally { await reader.cancel(); }
  const bytes = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
  return JSON.parse(new TextDecoder().decode(bytes));
}

export async function modelRequest(endpoint, path, payload) {
  if (!endpoint.apiKey || !endpoint.model) throw new Error('模型未配置');
  return responseJSON(await fetch(endpoint.baseUrl.replace(/\/$/, '') + path, {
    method: 'POST', headers: {'Authorization': `Bearer ${endpoint.apiKey}`, 'Content-Type': 'application/json'},
    body: JSON.stringify({...payload, model: endpoint.model}), signal: AbortSignal.timeout(60000), redirect: 'manual'
  }));
}

export async function embed(config, inputs) {
  const response = await modelRequest(config.embed, '/embeddings', {input: inputs});
  const vectors = new Array(inputs.length);
  for (const item of response.data || []) {
    if (!Number.isInteger(item.index) || item.index < 0 || item.index >= inputs.length || vectors[item.index] || !item.embedding?.length || !item.embedding.every(Number.isFinite)) throw new Error('嵌入响应格式错误');
    vectors[item.index] = item.embedding;
  }
  if (vectors.filter(Boolean).length !== inputs.length) throw new Error('嵌入向量数量错误');
  return vectors;
}

export async function queryEmbedding(env, config, query) {
  const keyBytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify([config.embed.baseUrl, config.embed.model, query.trim()])));
  const key = Array.from(new Uint8Array(keyBytes), value => value.toString(16).padStart(2, '0')).join('');
  const now = Date.now();
  const cached = await env.DB.prepare('SELECT vector FROM query_embeddings WHERE cache_key = ? AND expires_at > ?').bind(key, now).first();
  if (cached) return {vector: JSON.parse(cached.vector), cached: true};
  const [vector] = await embed(config, [query.trim()]);
  await env.DB.batch([
    env.DB.prepare('INSERT OR REPLACE INTO query_embeddings (cache_key, model, vector, expires_at, created_at) VALUES (?, ?, ?, ?, ?)').bind(key, config.embed.model, JSON.stringify(vector), now + 900000, now),
    env.DB.prepare('DELETE FROM query_embeddings WHERE expires_at <= ?').bind(now),
    env.DB.prepare('DELETE FROM query_embeddings WHERE cache_key IN (SELECT cache_key FROM query_embeddings ORDER BY created_at DESC LIMIT -1 OFFSET 256)')
  ]);
  return {vector, cached: false};
}

export async function astraRequest(config, command, collection = true) {
  const astra = config.astra;
  if (!astra.apiEndpoint || !astra.applicationToken) throw new Error('Astra DB 未配置');
  const endpoint = astra.apiEndpoint.replace(/\/$/, '') + '/api/json/v1/' + encodeURIComponent(astra.keyspace) + (collection ? '/' + encodeURIComponent(astra.collection) : '');
  const response = await responseJSON(await fetch(endpoint, {method: 'POST', headers: {'Token': astra.applicationToken, 'Content-Type': 'application/json'}, body: JSON.stringify(command), signal: AbortSignal.timeout(30000), redirect: 'manual'}));
  if (response.errors?.length) throw new Error(`Astra Data API: ${response.errors.map(error => error.errorCode).join(', ')}`);
  return response;
}

export async function deleteVectors(config, filter) {
  for (;;) {
    const result = await astraRequest(config, {deleteMany: {filter}});
    if (!result.status?.moreData) return;
  }
}

export async function writeVectors(config, prompt, vectors) {
  if (!vectors.length) return;
  const dimension = vectors[0].values.length;
  if (vectors.some(vector => vector.values.length !== dimension)) throw new Error('向量维度不一致');
  await astraRequest(config, {createCollection: {name: config.astra.collection, options: {vector: {dimension, metric: 'cosine'}}}}, false);
  const revision = `${prompt.id}:${prompt.derive_generation}`;
  for (let i = 0; i < vectors.length; i++) {
    const vector = vectors[i];
    const document = {_id: `${revision}:${vector.index ?? i}`, promptId: prompt.id, generation: prompt.derive_generation, revision, source: vector.source, start: vector.start, end: vector.end, model: config.embed.model, dimension, $vector: vector.values};
    await astraRequest(config, {findOneAndReplace: {filter: {_id: document._id}, replacement: document, options: {upsert: true}, projection: {_id: true}}});
  }
}

export async function meaningSearch(config, prompts, values, minimum, limit) {
  const hits = [];
  for (let start = 0; start < prompts.length; start += 50) {
    const batch = prompts.slice(start, start + 50);
    let revisions = batch.map(prompt => `${prompt.id}:${prompt.derive_generation}`);
    const byID = new Map(batch.map(prompt => [prompt.id, prompt]));
    let count = 0;
    while (revisions.length && count < limit) {
      const data = await astraRequest(config, {find: {
        filter: {revision: {$in: revisions}, model: config.embed.model, dimension: values.length},
        sort: {$vector: values}, projection: {promptId: true, generation: true, revision: true, start: true, end: true},
        options: {limit: 1000, includeSimilarity: true}
      }});
      const documents = data.data?.documents || [];
      const seen = new Set();
      for (const document of documents) {
        if (!revisions.includes(document.revision) || seen.has(document.revision)) continue;
        const prompt = byID.get(document.promptId);
        if (!prompt || document.generation !== prompt.derive_generation || !byteSlice(prompt.body, document.start, document.end)) continue;
        if (!Number.isFinite(document.$similarity)) throw new Error('Astra 返回结果缺少相似度');
        const score = 2 * document.$similarity - 1;
        if (score < minimum) break;
        seen.add(document.revision);
        hits.push({prompt, score, spans: [{start: document.start, end: document.end}]});
        count++;
        if (count >= limit) break;
      }
      if (!seen.size || documents.length < 1000) break;
      revisions = revisions.filter(revision => !seen.has(revision));
    }
  }
  return hits.sort((a, b) => b.score - a.score).slice(0, limit);
}

export async function match(env, config, query) {
  const results = await env.DB.batch([
    env.DB.prepare('SELECT * FROM prompts ORDER BY created_at DESC, id DESC'),
    env.DB.prepare('SELECT t.*, s.spelling FROM prompt_terms t LEFT JOIN prompt_spellings s ON s.term_id = t.id')
  ]);
  const prompts = results[0].results;
  if (!query.trim()) return {literal: prompts.map(prompt => ({prompt, spans: [], score: 0})), meaning: []};
  const terms = new Map();
  for (const row of results[1].results) {
    if (!terms.has(row.prompt_id)) terms.set(row.prompt_id, new Map());
    const group = terms.get(row.prompt_id);
    if (!group.has(row.id)) group.set(row.id, {...row, spellings: []});
    if (row.spelling) group.get(row.id).spellings.push(row.spelling);
  }
  const literal = prompts.map(prompt => literalMatch(prompt, [...(terms.get(prompt.id)?.values() || [])].filter(term => term.generation === prompt.derive_generation), query)).filter(Boolean);
  const literalIDs = new Set(literal.map(hit => hit.prompt.id));
  const candidates = prompts.filter(prompt => prompt.derive_status === 'ready' && !literalIDs.has(prompt.id));
  if (!candidates.length || !config.embed.apiKey || !config.embed.model || (config.meaningLimit ?? 5) <= 0) return {literal, meaning: []};
  const started = Date.now();
  try {
    const embeddingStarted = Date.now();
    const result = await queryEmbedding(env, config, query);
    const embeddingMs = Date.now() - embeddingStarted;
    const meaning = await meaningSearch(config, candidates, result.vector, queryMinimum(query, config), config.meaningLimit ?? 5);
    console.log(JSON.stringify({event: 'search', embeddingMs, cached: result.cached, totalMs: Date.now() - started}));
    return {literal, meaning};
  } catch (error) {
    console.error(JSON.stringify({event: 'search_failed', error: redact(error, config)}));
    return {literal, meaning: []};
  }
}
