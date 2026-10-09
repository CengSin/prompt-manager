## 1. Establish the multilingual baseline

- [x] 1.1 Build an isolated temporary library with representative English and other foreign-language prompts plus Chinese sentence queries; verify the configured embedding model's current top matches and scores, and report misses without changing the user's library or settings.

## 2. Return combined search results

- [x] 2.1 Update `Store.Match` to request a query embedding for every non-empty query when configured, even with literal hits; verify a focused store test returns literal hits when embedding is unavailable or fails.
- [x] 2.2 Exclude literal-hit IDs from scored semantic candidates before applying the meaning limit; verify focused tests cover literal-first order, semantic score order, threshold, no duplicate IDs, and filling all available semantic slots.
- [x] 2.3 Update the web search and detail tests for mixed literal and foreign-language semantic hits; verify the list shows both groups and the detail view highlights only stored original text, including escaped markup.

## 3. Validate and document the behavior

- [x] 3.1 Run `go test ./...` and verify all existing and new search, storage, and web tests pass.
- [x] 3.2 Repeat the isolated Chinese-sentence retrieval trial after the code change; verify and report target ranks, false positives, the configured threshold's effect, and search/detail latency. If quality is insufficient, document the failure and revisit the design before claiming cross-language retrieval works.
- [x] 3.3 Update `README.md` to explain Chinese-sentence search over foreign-language prompts, its dependence on a multilingual embedding model, and the extra query request; verify the instructions match observed behavior and do not imply that translation is stored.
