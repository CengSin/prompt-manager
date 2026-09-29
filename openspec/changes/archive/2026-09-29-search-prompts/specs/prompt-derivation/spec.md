## Purpose

在原文已经保存之后，为每条提示词准备关键词、气质词和向量，供按词和按意思找回，同时不改写用户贴入的原文。

## ADDED Requirements

### Requirement: Store the original before derivation
The system SHALL store submitted prompt text unchanged before derivation finishes. Derivation SHALL NOT change the stored text or the example URL.

#### Scenario: Text is available before derivation finishes
- **WHEN** the user submits valid prompt text
- **THEN** the system stores that text unchanged
- **THEN** the user can open the prompt before derivation finishes

### Requirement: Keep derived phrases grounded in the text
Each keyword and each 气质词 SHALL point at a contiguous span of the stored text. A 气质词 MAY record other spellings grouped with that span. A 气质词 SHALL use only the kinds style, medium, composition, lighting, and genre. The system SHALL NOT store a resolution, a duration, or a frame rate as a 气质词. A detail that is specific to one scene and is not one of those kinds SHALL be stored as a keyword when it is stored.

#### Scenario: A composition phrase is a 气质词 and the frame size is not
- **WHEN** derivation finishes for a prompt whose text contains `垂直构图` and `1920×1080`
- **THEN** `垂直构图` is stored as a 气质词 of kind composition and points at that span
- **THEN** `1920×1080` is not stored as a 气质词

#### Scenario: A grouped spelling points at the original span
- **WHEN** derivation groups `paper-cut` with a 气质词 whose span is `剪纸风格`
- **THEN** that 气质词 points at `剪纸风格`
- **THEN** `paper-cut` is recorded as another spelling of that 气质词

### Requirement: Store one vector per section, keyword, and 气质词
The system SHALL split the stored text into sections on blank lines and SHALL store one vector for each section. A heading line that only introduces the next section SHALL belong to that section. The system SHALL store one vector for each keyword and one vector for each 气质词.

#### Scenario: A blank line separates two section vectors
- **WHEN** derivation finishes for a prompt whose text has two blocks separated by a blank line
- **THEN** the prompt has a vector for each of those two blocks

#### Scenario: Each derived phrase has its own vector
- **WHEN** derivation finishes with one keyword and one 气质词
- **THEN** the prompt has a vector for that keyword and a vector for that 气质词, in addition to its section vectors

### Requirement: Leave literal search usable when derivation does not finish
When derivation has not finished or has failed, the system SHALL keep the stored text unchanged and SHALL NOT create semantic matches from missing vectors.

#### Scenario: Derivation fails
- **WHEN** derivation fails after a prompt is stored
- **THEN** the stored text is unchanged
- **THEN** a contiguous substring of that text still finds the prompt by literal search
- **THEN** the prompt does not appear under 「意思相近」

### Requirement: Refresh derived data when the text changes
When the user corrects the stored text, the system SHALL stop using keywords, 气质词, and vectors from the previous text and SHALL derive them again from the corrected text. Changing only the example URL SHALL leave the existing derived data in place. Deleting a prompt SHALL delete its derived data.

#### Scenario: Correcting the text drops stale derived data
- **WHEN** the user replaces a prompt's text and searches for a keyword that existed only on the previous text
- **THEN** that keyword does not make the prompt a literal match

#### Scenario: An example-URL-only correction keeps derived data
- **WHEN** the user clears a prompt's example URL and leaves its text unchanged
- **THEN** the prompt's existing keywords, 气质词, and vectors remain

#### Scenario: Deletion removes derived data
- **WHEN** the user confirms deletion of a prompt
- **THEN** that prompt's keywords, 气质词, and vectors are removed

### Requirement: Read model settings from a local configuration file
The system SHALL read term-extraction settings and embedding settings from a local configuration file. Each side SHALL have its own base URL, API key, and model. The system SHALL NOT read those values, the similarity minimum, or the meaning-result cap from environment variables. When a base URL is omitted, it SHALL be `https://openrouter.ai/api/v1`. An API key and a model SHALL have no default. When the file is missing, cannot be read, or a side has no API key or no model, that side SHALL be not configured.

#### Scenario: A missing file leaves both sides unconfigured
- **WHEN** the configuration file is absent and the user saves a prompt
- **THEN** the system does not send a term-extraction request
- **THEN** the system does not send an embedding request
- **THEN** that prompt is not listed as failed

#### Scenario: Environment variables do not supply a model
- **WHEN** environment variables name a base URL, an API key, and a model, and the configuration file does not
- **THEN** the system does not send a term-extraction request
- **THEN** the system does not send an embedding request

### Requirement: Call embeddings through a configurable endpoint
The system SHALL request embeddings with `POST {baseURL}/embeddings` using an OpenAI-compatible JSON body. The base URL, API key, and model SHALL come from the local configuration file. The default base URL SHALL be `https://openrouter.ai/api/v1`. The API key and the model SHALL have no default. When the embedding API key or the embedding model is missing, the system SHALL NOT send an embedding request and SHALL NOT mark prompts failed only because that configuration is missing.

#### Scenario: The configured endpoint receives the selected model
- **WHEN** the configuration file sets an embedding base URL, an API key, and a model, and derivation embeds text
- **THEN** the request is sent to that base URL's `/embeddings` path
- **THEN** the request authenticates with that API key
- **THEN** the request names that model
- **THEN** the request does not include the example URL

#### Scenario: Missing embedding configuration sends nothing
- **WHEN** the configuration file has no embedding API key or no embedding model and the user saves a prompt
- **THEN** the system does not send an embedding request
- **THEN** that prompt is not listed as failed

### Requirement: Call term extraction through a configurable endpoint
The system SHALL request term extraction with `POST {baseURL}/chat/completions` using an OpenAI-compatible JSON body that names the configured model and includes the stored text. The system SHALL read the model text from the completion message content. The base URL, API key, and model SHALL come from the local configuration file. The default base URL SHALL be `https://openrouter.ai/api/v1`. The API key and the model SHALL have no default. The request SHALL NOT include the example URL. When the term-extraction API key or model is missing, the system SHALL NOT send a term-extraction request and SHALL NOT mark prompts failed only because that configuration is missing.

#### Scenario: The configured chat endpoint receives the selected model
- **WHEN** the configuration file sets a term-extraction base URL, an API key, and a model, and term extraction runs
- **THEN** the request is sent to that base URL's `/chat/completions` path
- **THEN** the request authenticates with that API key
- **THEN** the request names that model
- **THEN** the request includes the stored text
- **THEN** the request does not include the example URL

#### Scenario: Missing term configuration sends nothing
- **WHEN** the configuration file has no term-extraction API key or no term-extraction model and the user saves a prompt
- **THEN** the system does not send that prompt's text for term extraction
- **THEN** that prompt is not listed as failed
- **THEN** a contiguous substring of the text still finds the prompt

### Requirement: Review and retry failed derivations
When a derivation attempt fails, the system SHALL record the prompt as failed, SHALL store the time of the failure, and SHALL store a reason that does not contain an API key. The stored text SHALL stay unchanged. The system SHALL provide one page that lists every failed prompt with an excerpt of its text, the reason, and the failure time. That page SHALL NOT list a prompt whose derivation has not failed. The library list SHALL link to the page when at least one prompt has failed. The library list SHALL NOT offer that link when no prompt has failed. The user SHALL be able to retry one listed prompt or every listed prompt. Starting a retry SHALL remove the prompt from the failed list while the attempt is in progress. A failed retry SHALL put the prompt back on the list with the new reason and time. A successful retry SHALL leave the prompt off the list. Deleting a failed prompt SHALL remove it from the list.

#### Scenario: A failed attempt appears on the failure page
- **WHEN** derivation fails for a stored prompt
- **THEN** the failure page shows that prompt with an excerpt, a reason, and a time
- **THEN** the reason does not contain the API key
- **THEN** the stored text is unchanged

#### Scenario: A prompt that has not failed stays off the page
- **WHEN** a prompt's derivation has not been attempted or is still in progress
- **THEN** the failure page does not list that prompt

#### Scenario: Retrying one prompt
- **WHEN** the user retries one failed prompt and the new attempt fails
- **THEN** the prompt leaves the failure page while the attempt runs
- **THEN** the prompt returns to the failure page with the new failure

#### Scenario: A successful retry leaves the failure page
- **WHEN** the user retries one failed prompt and the new attempt succeeds
- **THEN** the failure page no longer lists that prompt

#### Scenario: Retrying every failed prompt
- **WHEN** several prompts have failed and the user retries all of them
- **THEN** derivation runs again for each of those prompts

#### Scenario: Retry without embedding configuration stays failed
- **WHEN** a prompt is listed as failed and the configuration file no longer has an embedding API key or model
- **THEN** retrying it does not send an embedding request
- **THEN** the failure page still lists that prompt

#### Scenario: Retry without term configuration stays failed
- **WHEN** a prompt is listed as failed and the configuration file no longer has a term-extraction API key or model
- **THEN** retrying it does not send a term-extraction request
- **THEN** the failure page still lists that prompt

#### Scenario: The library links to the failure page
- **WHEN** at least one prompt has failed
- **THEN** the library list links to the failure page

#### Scenario: The library hides the link when nothing failed
- **WHEN** no prompt has failed
- **THEN** the library list does not link to the failure page

#### Scenario: Deleting a failed prompt removes it from the failure page
- **WHEN** the user confirms deletion of a prompt listed as failed
- **THEN** the failure page no longer lists that prompt
