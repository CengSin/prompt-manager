import {WorkflowEntrypoint} from 'cloudflare:workers';
import {handle} from './app.mjs';
import {configuration, embed, modelRequest, redact, writeVectors, deleteVectors} from './services.mjs';
import {sections, parseTerms} from './text.mjs';

export class PromptDerivation extends WorkflowEntrypoint {
  async run(event, step) {
    const {id, generation} = event.payload;
    const config = configuration(this.env);
    const options = {retries: {limit: 2, delay: '5 seconds', backoff: 'exponential'}, timeout: '2 minutes'};
    try {
      const prompt = await step.do('load-current-prompt', () => this.env.DB.prepare('SELECT * FROM prompts WHERE id = ? AND derive_generation = ?').bind(id, generation).first());
      if (!prompt) return {stale: true};
      const terms = await step.do('extract-terms', options, async () => {
        const text = '从下面的提示词原文抽出 JSON，不要解释。keywords 是找回这一条用的说法，temperament 是气质词。kind 只能是 style、medium、composition、lighting、genre。phrase 必须是原文里的连续子串。spellings 是同一意思的其他写法。不要把分辨率、时长、帧率放进 temperament。格式 {"keywords":[{"phrase":""}],"temperament":[{"kind":"composition","phrase":"","spellings":[]}]}\n原文：\n' + prompt.body;
        const response = await modelRequest(config.term, '/chat/completions', {messages: [{role: 'user', content: text}], temperature: 0});
        return parseTerms(prompt.body, response.choices?.[0]?.message?.content || '{}');
      });
      const segments = [...sections(prompt.body).map(section => ({...section, source: 'section'})), ...terms.map(term => ({text: term.phrase, start: term.start, end: term.end, source: 'term'}))];
      for (let index = 0; index < segments.length; index += 8) {
        const batch = segments.slice(index, index + 8);
        const values = await step.do(`embed-${index}`, options, () => embed(config, batch.map(segment => segment.text)));
        await step.do(`save-vectors-${index}`, options, async () => {
          const current = await this.env.DB.prepare('SELECT id FROM prompts WHERE id = ? AND derive_generation = ?').bind(id, generation).first();
          if (!current) return;
          const vectors = batch.map((segment, i) => ({...segment, values: values[i]}));
          await writeVectors(config, prompt, vectors.map((vector, i) => ({...vector, index: index + i})));
        });
      }
      await step.do('commit-derived-terms', options, async () => {
        const current = await this.env.DB.prepare('SELECT id FROM prompts WHERE id = ? AND derive_generation = ?').bind(id, generation).first();
        if (!current) { await deleteVectors(config, {revision: `${id}:${generation}`}); return; }
        const guard = 'EXISTS (SELECT 1 FROM prompts WHERE id = ? AND derive_generation = ?)';
        const statements = [
          this.env.DB.prepare(`DELETE FROM prompt_spellings WHERE term_id IN (SELECT id FROM prompt_terms WHERE prompt_id = ?) AND ${guard}`).bind(id, id, generation),
          this.env.DB.prepare(`DELETE FROM prompt_terms WHERE prompt_id = ? AND ${guard}`).bind(id, id, generation)
        ];
        for (let i = 0; i < terms.length; i++) {
          const term = terms[i];
          const termID = `${id}:${generation}:term:${i}`;
          statements.push(this.env.DB.prepare(`INSERT INTO prompt_terms (id, prompt_id, kind, phrase, start, end, generation) SELECT ?, ?, ?, ?, ?, ?, ? WHERE ${guard}`).bind(termID, id, term.kind, term.phrase, term.start, term.end, generation, id, generation));
          for (const spelling of term.spellings) statements.push(this.env.DB.prepare(`INSERT INTO prompt_spellings (term_id, spelling) SELECT ?, ? WHERE ${guard}`).bind(termID, spelling, id, generation));
        }
        statements.push(this.env.DB.prepare("UPDATE prompts SET derive_status = 'ready', derive_error = NULL, derive_failed_at = NULL WHERE id = ? AND derive_generation = ?").bind(id, generation));
        await this.env.DB.batch(statements);
      });
      return {ready: true};
    } catch (error) {
      await step.do('mark-failed', async () => {
        const reason = redact(error, config);
        console.error(JSON.stringify({event: 'derive_failed', id, reason}));
        await this.env.DB.prepare("UPDATE prompts SET derive_status = 'failed', derive_error = ?, derive_failed_at = ? WHERE id = ? AND derive_generation = ?").bind(reason, new Date().toISOString(), id, generation).run();
      });
      return {failed: true};
    }
  }
}

export default {
  async fetch(request, env) {
    try { return await handle(request, env); }
    catch (error) {
      console.error(JSON.stringify({event: 'request_failed', error: redact(error, configuration(env))}));
      return new Response('处理失败，请稍后重试。', {status: 500, headers: {'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store'}});
    }
  }
};
