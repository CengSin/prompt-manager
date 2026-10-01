const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', {fatal: true});

export function escapeHTML(value = '') {
  return String(value).replace(/[&<>"']/g, char => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[char]));
}

export function byteOffset(text, index) {
  return encoder.encode(text.slice(0, index)).length;
}

export function byteSlice(text, start, end) {
  const bytes = encoder.encode(text);
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 0 || end < start || end > bytes.length) return '';
  try { return decoder.decode(bytes.slice(start, end)); } catch { return ''; }
}

export function findAll(body, needle) {
  if (!needle) return [];
  const chars = Array.from(body);
  const target = Array.from(needle.toLowerCase());
  const spans = [];
  let offset = 0;
  for (let i = 0; i < chars.length; i++) {
    if (target.every((char, j) => chars[i + j]?.toLowerCase() === char)) {
      const length = encoder.encode(chars.slice(i, i + target.length).join('')).length;
      spans.push({start: offset, end: offset + length});
      for (let j = 0; j < target.length; j++) offset += encoder.encode(chars[i + j]).length;
      i += target.length - 1;
    } else offset += encoder.encode(chars[i]).length;
  }
  return spans;
}

export function mergeSpans(spans) {
  const out = [];
  for (const span of [...spans].sort((a, b) => a.start - b.start || a.end - b.end)) {
    const last = out.at(-1);
    if (last && span.start <= last.end) last.end = Math.max(last.end, span.end);
    else out.push({...span});
  }
  return out;
}

export function markHTML(body, spans) {
  const bytes = encoder.encode(body);
  let offset = 0;
  let result = '';
  for (const span of mergeSpans(spans)) {
    if (span.start < offset || span.end > bytes.length || span.end < span.start || !byteSlice(body, span.start, span.end)) continue;
    result += escapeHTML(decoder.decode(bytes.slice(offset, span.start))) + '<mark>' + escapeHTML(decoder.decode(bytes.slice(span.start, span.end))) + '</mark>';
    offset = span.end;
  }
  return result + escapeHTML(decoder.decode(bytes.slice(offset)));
}

export function excerptHTML(body, spans = []) {
  if (!spans.length) {
    const chars = Array.from(body.replace(/\r\n|\r|\n/g, ' '));
    return escapeHTML(chars.slice(0, 120).join('') + (chars.length > 120 ? '…' : ''));
  }
  const bytes = encoder.encode(body);
  const earliest = Math.max(0, Math.min(bytes.length, Math.min(...spans.map(span => span.start))));
  const at = Array.from(decoder.decode(bytes.slice(0, earliest))).length;
  const chars = Array.from(body);
  const from = Math.max(0, at - 20);
  const to = Math.min(chars.length, from + 120);
  const start = encoder.encode(chars.slice(0, from).join('')).length;
  const end = encoder.encode(chars.slice(0, to).join('')).length;
  const local = spans.filter(span => span.end > start && span.start < end).map(span => ({start: Math.max(span.start, start) - start, end: Math.min(span.end, end) - start}));
  return (from > 0 ? '…' : '') + markHTML(chars.slice(from, to).join(''), local) + (to < chars.length ? '…' : '');
}

export function sections(body) {
  const blocks = [];
  const separator = /\n[ \t\r]*\n(?:[ \t\r]*\n)*/g;
  let start = 0;
  for (const match of body.matchAll(separator)) {
    if (body.slice(start, match.index).trim()) blocks.push({text: body.slice(start, match.index), start: byteOffset(body, start), end: byteOffset(body, match.index)});
    start = match.index + match[0].length;
  }
  if (body.slice(start).trim()) blocks.push({text: body.slice(start), start: byteOffset(body, start), end: byteOffset(body, body.length)});
  const out = [];
  for (let i = 0; i < blocks.length; i++) {
    const block = blocks[i];
    const trimmed = block.text.trim();
    if (!trimmed.includes('\n') && (/[:：]$/.test(trimmed) || /^【.*】$/.test(trimmed)) && blocks[i + 1]) {
      out.push({text: byteSlice(body, block.start, blocks[i + 1].end), start: block.start, end: blocks[i + 1].end});
      i++;
    } else out.push(block);
  }
  return out;
}

export function parseTerms(body, raw) {
  const document = JSON.parse(raw.trim().replace(/^```(?:json)?\s*/, '').replace(/\s*```$/, ''));
  const kinds = {style: 'style', '风格': 'style', medium: 'medium', '媒介': 'medium', composition: 'composition', '构图': 'composition', lighting: 'lighting', '光线': 'lighting', genre: 'genre', '体裁': 'genre'};
  const terms = [];
  const delivery = /^(?:\d+\s*[x×]\s*\d+|\d+(?:\.\d+)?\s*fps|\d+\s*秒|\d+\s*分(?:\s*\d+\s*秒)?)$/i;
  const ground = (item, kind) => {
    const phrase = String(item.phrase || '').trim();
    const at = body.indexOf(phrase);
    if (!phrase || at < 0) return;
    const spellings = [...new Set((item.spellings || []).map(value => String(value).trim()).filter(value => value && value.toLowerCase() !== phrase.toLowerCase()))];
    terms.push({kind, phrase, start: byteOffset(body, at), end: byteOffset(body, at + phrase.length), spellings});
  };
  for (const item of document.keywords || []) ground({...item, spellings: []}, 'keyword');
  for (const item of document.temperament || []) {
    const kind = kinds[item.kind];
    if (kind && !delivery.test(String(item.phrase || '').trim())) ground(item, kind);
  }
  return terms;
}

export function literalMatch(prompt, terms, query) {
  const pieces = query.trim().split(/\s+/).filter(Boolean);
  const contains = (text, needle) => String(text).toLowerCase().includes(needle.toLowerCase());
  if (!pieces.length || !pieces.every(piece => contains(prompt.body, piece) || terms.some(term => contains(term.phrase, piece) || term.spellings.some(spelling => contains(spelling, piece))))) return null;
  const spans = [];
  for (const piece of pieces) {
    const bodySpans = findAll(prompt.body, piece);
    if (bodySpans.length) { spans.push(...bodySpans); continue; }
    for (const term of terms) {
      if (term.spellings.some(spelling => contains(spelling, piece))) {
        spans.push({start: term.start, end: term.end}, ...findAll(prompt.body, term.phrase));
      }
    }
  }
  return {prompt, spans: mergeSpans(spans), score: 0};
}

export function queryMinimum(query, config) {
  const length = Array.from(query.trim()).length;
  const min = config.similarityMin ?? 0.35;
  return length > 0 && length <= 4 ? Math.max(min, config.shortQuerySimilarityMin ?? 0.60) : min;
}

export function validateDraft(body, exampleURL) {
  if (!body.trim()) throw new Error('原文必填');
  const example = exampleURL.trim();
  if (example) {
    let url;
    try { url = new URL(example); } catch { throw new Error('示例链接必须是 http 或 https 链接'); }
    if (!['http:', 'https:'].includes(url.protocol)) throw new Error('示例链接必须是 http 或 https 链接');
  }
  return {body, example_url: example || null, body_search: body.toLowerCase()};
}
