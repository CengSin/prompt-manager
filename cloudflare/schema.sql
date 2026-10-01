CREATE TABLE IF NOT EXISTS prompts (
		id TEXT PRIMARY KEY,
		body TEXT NOT NULL,
		body_search TEXT NOT NULL,
		example_url TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	, derive_generation INTEGER NOT NULL DEFAULT 0, derive_status TEXT NOT NULL DEFAULT 'none', derive_error TEXT, derive_failed_at TEXT);

CREATE TABLE IF NOT EXISTS prompt_terms (
			id TEXT PRIMARY KEY,
			prompt_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			phrase TEXT NOT NULL,
			start INTEGER NOT NULL,
			end INTEGER NOT NULL,
			generation INTEGER NOT NULL
		);

CREATE TABLE IF NOT EXISTS prompt_spellings (
			term_id TEXT NOT NULL,
			spelling TEXT NOT NULL
		);

CREATE TABLE IF NOT EXISTS prompt_vectors (
			id TEXT PRIMARY KEY,
			prompt_id TEXT NOT NULL,
			source TEXT NOT NULL,
			source_id TEXT,
			start INTEGER NOT NULL,
			end INTEGER NOT NULL,
			model TEXT NOT NULL,
			dim INTEGER NOT NULL,
			vector BLOB NOT NULL,
			generation INTEGER NOT NULL
		);

CREATE TABLE IF NOT EXISTS query_embeddings (cache_key TEXT PRIMARY KEY, model TEXT NOT NULL, vector TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);

