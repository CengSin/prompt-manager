import {style} from './style.mjs';
import {escapeHTML, excerptHTML, markHTML, validateDraft} from './text.mjs';
import {configuration, deleteVectors, match, redact} from './services.mjs';

function page(title, content, refresh = false, status = 200) {
  return new Response(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">${refresh ? '<meta http-equiv="refresh" content="3">' : ''}<link rel="icon" href="data:,"><title>${escapeHTML(title)}</title><style>${style}</style></head><body><header><h1><a href="/">提示词</a></h1><a href="/prompts/new">贴上一条</a></header><main>${content}</main></body></html>`, {
    status, headers: {'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff', 'Referrer-Policy': 'same-origin', 'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'; img-src data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"}
  });
}

function redirect(path) {
  return new Response(null, {status: 303, headers: {Location: path, 'Cache-Control': 'no-store'}});
}

function form(prompt = {}, error = '') {
  const action = prompt.id ? `/prompts/${prompt.id}` : '/prompts';
  return page(prompt.id ? '改正' : '贴上一条', `<form class="editor" method="post" action="${action}">${error ? `<p class="error">${escapeHTML(error)}</p>` : ''}<label>原文<textarea name="body">${escapeHTML(prompt.body || '')}</textarea></label><label>示例链接<input name="example_url" value="${escapeHTML(prompt.example_url || '')}"></label><div class="actions"><button type="submit">保存</button>${prompt.id ? `<a href="/prompts/${prompt.id}">取消</a>` : ''}</div></form>`);
}

function article(hit, query = '') {
  const prompt = hit.prompt;
  const suffix = query ? '?q=' + encodeURIComponent(query) : '';
  return `<article><a href="/prompts/${prompt.id}${suffix}">${excerptHTML(prompt.body, hit.spans)}</a>${prompt.example_url ? `<p><a href="${escapeHTML(prompt.example_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(prompt.example_url)}</a></p>` : ''}</article>`;
}

export function ready(config) {
  return Boolean(config.term.apiKey && config.term.model && config.embed.apiKey && config.embed.model && config.astra.apiEndpoint && config.astra.applicationToken);
}

export async function enqueue(env, config, id) {
  if (!ready(config)) return;
  const prompt = await env.DB.prepare('SELECT * FROM prompts WHERE id = ?').bind(id).first();
  if (!prompt) return;
  await env.DB.prepare("UPDATE prompts SET derive_status = 'pending', derive_error = NULL, derive_failed_at = NULL WHERE id = ? AND derive_generation = ?").bind(id, prompt.derive_generation).run();
  try {
    await env.DERIVE_WORKFLOW.create({id: `${id}-${prompt.derive_generation}-${crypto.randomUUID()}`, params: {id, generation: prompt.derive_generation}});
  } catch (error) {
    await env.DB.prepare("UPDATE prompts SET derive_status = 'failed', derive_error = ?, derive_failed_at = ? WHERE id = ? AND derive_generation = ?").bind(redact(error, config), new Date().toISOString(), id, prompt.derive_generation).run();
    throw error;
  }
}

function clearDerived(env, id) {
  return [
    env.DB.prepare('DELETE FROM prompt_spellings WHERE term_id IN (SELECT id FROM prompt_terms WHERE prompt_id = ?)').bind(id),
    env.DB.prepare('DELETE FROM prompt_terms WHERE prompt_id = ?').bind(id)
  ];
}

async function readForm(request) {
  const reader = request.body?.getReader();
  const chunks = [];
  let length = 0;
  if (reader) {
    try {
      for (;;) {
        const {done, value} = await reader.read();
        if (done) break;
        length += value.length;
        if (length > 3 * 1024 * 1024) throw new Error('提交内容过长');
        chunks.push(value);
      }
    } finally { await reader.cancel(); }
  }
  const bytes = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
  const text = new TextDecoder().decode(bytes);
  const values = new URLSearchParams(text);
  const body = values.get('body') || '';
  const example = values.get('example_url') || '';
  return {body, example_url: example};
}

export async function handle(request, env) {
  const url = new URL(request.url);
  const path = url.pathname;
  const config = configuration(env);
  if (request.method === 'POST') {
    const origin = request.headers.get('Origin') || request.headers.get('Referer');
    let allowed = false;
    try { allowed = new URL(origin).origin === url.origin; } catch {}
    if (!allowed) return page('拒绝提交', '<p>拒绝来自其他网站的提交</p>', false, 403);
    if (!(request.headers.get('Content-Type') || '').startsWith('application/x-www-form-urlencoded')) return page('提交格式错误', '<p>提交格式错误</p>', false, 415);
  }
  if (!['GET', 'HEAD', 'POST'].includes(request.method)) return new Response('Method not allowed', {status: 405});
  if (path === '/healthz') return Response.json({ok: true});
  if (path === '/' && request.method !== 'POST') {
    const query = url.searchParams.get('q') || '';
    const result = await match(env, config, query);
    const failed = await env.DB.prepare("SELECT COUNT(*) AS n FROM prompts WHERE derive_status = 'failed'").first();
    const content = `<form class="search" method="get" action="/"><label>搜索<input id="q" name="q" value="${escapeHTML(query)}"></label><button type="submit">搜索</button></form>${failed.n ? `<p><a href="/prompts/failed">${failed.n} 条处理失败</a></p>` : ''}${!result.literal.length && !result.meaning.length ? `<p class="empty">${query.trim() ? '没有匹配的提示词' : '还没有提示词'}</p>` : ''}${result.literal.map(hit => article(hit, query)).join('')}${result.meaning.length ? '<h2>意思相近</h2>' : ''}${result.meaning.map(hit => article(hit, query)).join('')}`;
    return page('提示词', content);
  }
  if (path === '/prompts/new' && request.method !== 'POST') return form();
  if (path === '/prompts' && request.method === 'POST') {
    const values = await readForm(request);
    let draft;
    try { draft = validateDraft(values.body, values.example_url); } catch (error) { return form(values, error.message); }
    const id = crypto.randomUUID();
    const now = new Date().toISOString();
    await env.DB.prepare('INSERT INTO prompts (id, body, body_search, example_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)').bind(id, draft.body, draft.body_search, draft.example_url, now, now).run();
    await enqueue(env, config, id);
    return redirect(`/prompts/${id}`);
  }
  if (path === '/prompts/failed' && request.method !== 'POST') {
    const result = await env.DB.prepare("SELECT * FROM prompts WHERE derive_status IN ('failed', 'pending') ORDER BY derive_failed_at DESC, id DESC").all();
    const failed = result.results.filter(prompt => prompt.derive_status === 'failed');
    const pending = result.results.some(prompt => prompt.derive_status === 'pending');
    const content = `${pending ? '<p class="empty">正在重跑，这一页会自动更新。</p>' : ''}${!failed.length && !pending ? '<p class="empty">没有处理失败</p>' : ''}${failed.length ? '<form method="post" action="/prompts/failed/retry"><button type="submit">全部重跑</button></form>' : ''}${failed.map(prompt => `<article><a href="/prompts/${prompt.id}">${excerptHTML(prompt.body)}</a><p>${escapeHTML(prompt.derive_error || '')}</p><p>${escapeHTML(prompt.derive_failed_at || '')}</p><form method="post" action="/prompts/${prompt.id}/derive"><button type="submit">重跑</button></form></article>`).join('')}`;
    return page('处理失败', content, pending);
  }
  if (path === '/prompts/failed/retry' && request.method === 'POST') {
    const result = await env.DB.prepare("SELECT id FROM prompts WHERE derive_status = 'failed'").all();
    for (const prompt of result.results) await enqueue(env, config, prompt.id);
    return redirect('/prompts/failed');
  }
  const route = path.match(/^\/prompts\/([a-zA-Z0-9-]+)(?:\/(edit|delete|derive))?$/);
  if (!route) return page('未找到', '<p class="empty">未找到页面</p>', false, 404);
  const [, id, action] = route;
  const prompt = await env.DB.prepare('SELECT * FROM prompts WHERE id = ?').bind(id).first();
  if (!prompt) return page('未找到', '<p class="empty">未找到这条提示词</p>', false, 404);
  if (request.method === 'POST' && action === 'derive') { await enqueue(env, config, id); return redirect('/prompts/failed'); }
  if (request.method === 'POST' && action === 'delete') {
    await deleteVectors(config, {promptId: id});
    await env.DB.batch([...clearDerived(env, id), env.DB.prepare('DELETE FROM prompts WHERE id = ?').bind(id)]);
    return redirect('/');
  }
  if (request.method === 'POST' && !action) {
    const values = await readForm(request);
    let draft;
    try { draft = validateDraft(values.body, values.example_url); } catch (error) { return form({...values, id}, error.message); }
    if (draft.body === prompt.body) {
      await env.DB.prepare('UPDATE prompts SET example_url = ?, updated_at = ? WHERE id = ?').bind(draft.example_url, new Date().toISOString(), id).run();
    } else {
      await deleteVectors(config, {revision: `${id}:${prompt.derive_generation}`});
      await env.DB.batch([
        ...clearDerived(env, id),
        env.DB.prepare("UPDATE prompts SET body = ?, body_search = ?, example_url = ?, updated_at = ?, derive_generation = derive_generation + 1, derive_status = 'none', derive_error = NULL, derive_failed_at = NULL WHERE id = ?").bind(draft.body, draft.body_search, draft.example_url, new Date().toISOString(), id)
      ]);
      await enqueue(env, config, id);
    }
    return redirect(`/prompts/${id}`);
  }
  if (request.method === 'POST') return new Response('Method not allowed', {status: 405});
  if (action === 'edit') return form(prompt);
  if (action === 'delete') return page('删除提示词', `<p>删除后无法恢复。</p><pre class="body">${excerptHTML(prompt.body)}</pre><form class="confirm" method="post" action="/prompts/${id}/delete"><div class="actions"><button type="submit">确认删除</button><a href="/prompts/${id}">取消</a></div></form>`);
  if (action) return page('未找到', '<p class="empty">未找到页面</p>', false, 404);
  const query = url.searchParams.get('q') || '';
  let rendered = escapeHTML(prompt.body);
  if (query.trim()) {
    const result = await match(env, config, query);
    const hit = [...result.literal, ...result.meaning].find(hit => hit.prompt.id === id);
    if (hit) rendered = markHTML(prompt.body, hit.spans);
  }
  return page('提示词', `<pre class="body">${rendered}</pre>${prompt.example_url ? `<p>当初的作品</p><p><a href="${escapeHTML(prompt.example_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(prompt.example_url)}</a></p>` : ''}<div class="actions"><a href="/prompts/${id}/edit">改正</a><a href="/prompts/${id}/delete">删除</a></div>`);
}
