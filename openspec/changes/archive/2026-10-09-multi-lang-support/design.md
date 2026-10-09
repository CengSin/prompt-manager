## Context

See `proposal.md` for the user problem. `prompt-derivation` already stores vectors for each original-text section, keyword, and 气质词. `Store.Match` returns immediately on any literal hit, so its embedding branch never sees mixed literal and semantic results. The list template can already render both result groups; the detail handler recomputes the match to highlight the selected original-text span. The embedding endpoint is configurable; this use case requires a multilingual model and representative foreign-language examples for evaluation.

## Goals / Non-Goals

**Goals:**
- Reuse the existing vectors and configurable embedding endpoint for Chinese-sentence retrieval across languages.
- Preserve literal ranking and exact original-text highlighting while exposing additional relevant semantic hits.
- Verify behavior with deterministic search tests and a small representative multilingual retrieval trial.

**Non-Goals:**
- Machine-translate or persist prompt text, add language detection, or build a Chinese alias index.
- Require a particular embedding provider or silently change the user's configured model or similarity threshold.
- Guarantee cross-language recall when the configured model lacks that capability or vectors are absent.

## Decisions

### Run semantic retrieval for every non-empty query when configured

`Store.Match` will compute literal hits first and also request one query embedding when embedding is configured and the meaning-result cap is positive. It will compare that vector against stored section and term vectors even when literal hits exist. Reuse `search.Meaning` and its model/dimension checks. Keep the current behavior for blank queries and for embedding failures: return the library or literal hits without an error page.

Alternative considered: translate the Chinese query to each prompt's language. That adds language detection and translation services, can shift prompt terminology, and is unnecessary for an initial trial using the existing multilingual embedding path. Another alternative is keeping semantic search as a fallback, which cannot retrieve foreign-language prompts when a Chinese prompt matches literally.

### Deduplicate before applying the meaning cap

Collect eligible semantic candidates in similarity order, remove IDs already in the literal group, then take up to the configured meaning limit. Literal hits retain creation order. Do this after scoring, not after taking the first N semantic candidates, so a literal hit cannot consume a semantic slot. The existing `Prompts` and `Meaning` groups in the page can render the combined result without a new control.

Alternative considered: merge both groups into one score-ranked list. That would weaken the current predictable ordering of exact matches and obscure why a result appeared.

### Keep highlights tied to the stored original

Reuse the winning vector's stored span for semantic excerpts and detail highlights. Deduplicated literal hits use literal spans. Do not insert a Chinese translation into the excerpt or modify saved text. Existing HTML escaping remains in the rendering path.

### Validate multilingual quality separately from deterministic behavior

Use tests with controlled vectors to prove ordering, deduplication, cap, errors, and original spans. For model quality, use a temporary test library with several foreign-language prompts and Chinese sentence queries, then record relevant rank, omissions, and false positives at the configured threshold. This trial evaluates the configured model; deterministic tests cannot prove its semantic quality. Keep the threshold configurable and adjust it only from observed examples. If quality is insufficient, revisit a query-instruction or translated-index design in a separate decision instead of adding one untested here.

## Risks / Trade-offs

- Every non-empty search can issue an embedding request, increasing latency and model charges. → Reuse one query embedding per search call, keep the configured cap, and measure the trial's latency; document this behavior.
- The detail page recomputes the query and may send a second embedding request after a result click. → Accept this existing flow for the initial change, measure it, and optimize only if the added cost is material.
- Similarity scores vary by model and language, so the current minimum may omit good foreign matches or admit weak ones. → Record multilingual trial outcomes and tune the existing local setting from evidence, without hard-coding a model-specific threshold.
- A query embedding failure can hide semantic hits. → Continue showing literal hits and keep missing-vector behavior unchanged.

## Migration Plan

No schema migration or backfill is needed for prompts with current-model vectors. First record a baseline with a temporary multilingual library, then deploy the search and test changes and repeat the trial. Rollback is a code revert; stored prompts and vectors remain valid.
