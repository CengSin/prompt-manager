## MODIFIED Requirements

### Requirement: Search the pasted text
The system SHALL match a query that contains non-whitespace characters by splitting it on whitespace and ignoring empty pieces. A prompt SHALL be a literal match only when every piece occurs as a contiguous substring of that prompt's stored text, of one of its keywords, or of any spelling of one of its 气质词. Latin letters SHALL match regardless of case. Non-Latin text, including Chinese, SHALL match by exact substring. Search SHALL NOT use the example URL, user-managed categories, or user-managed tags as the match corpus. A query that is empty or only whitespace SHALL present the same list as the unfiltered library list. Literal matches SHALL keep the library list order.

For a non-empty query, when query embeddings are available, the system SHALL embed the query as a whole and compare it with each prompt's section vectors, keyword vectors, and 气质词 vectors, regardless of whether literal matches exist. A prompt's score SHALL be the highest of those similarities. The system SHALL show literal matches first, followed by prompts whose score is at or above the configured minimum under the label 「意思相近」, ordered from highest score to lowest. A prompt already shown as a literal match SHALL NOT also appear under 「意思相近」. The system SHALL apply the configured meaning-result cap after excluding literal matches. If query embeddings are unavailable or fail, the system SHALL still show literal matches. When no literal or eligible meaning match exists, the system SHALL show that nothing matched.

#### Scenario: Match a Chinese substring
- **WHEN** a stored prompt's text contains `剪纸` and the user searches for `剪纸`
- **THEN** that prompt appears in the literal results

#### Scenario: Match Latin letters regardless of case
- **WHEN** a stored prompt's text contains `Paper-cut` and the user searches for `paper-cut`
- **THEN** that prompt appears in the literal results

#### Scenario: Match every space-separated piece
- **WHEN** one stored prompt's text contains `垂直构图` and `海滩` in different places, another stored prompt's text contains only `海滩`, and the user searches for `垂直 海滩`
- **THEN** the first prompt appears in the literal results
- **THEN** the second prompt is absent from the literal results

#### Scenario: Match a grouped spelling
- **WHEN** a prompt has a 气质词 whose text span is `剪纸风格` and whose grouped spelling includes `paper-cut`, and the user searches for `paper-cut`
- **THEN** that prompt appears in the literal results

#### Scenario: Do not match the example URL alone
- **WHEN** the query occurs only in a prompt's example URL, does not occur in its text, keywords, or 气质词, and none of that prompt's vectors reaches the similarity minimum
- **THEN** that prompt is absent from the results

#### Scenario: Blank search lists the library
- **WHEN** the user searches with an empty or whitespace-only query
- **THEN** the system shows the same prompts in the same order as the unfiltered list
- **THEN** the system does not request a query embedding

#### Scenario: A Chinese description finds a foreign-language prompt
- **WHEN** a user searches with a Chinese sentence describing a stored foreign-language prompt and that prompt's highest similarity reaches the configured minimum
- **THEN** that prompt appears under 「意思相近」, including when another prompt matches the query literally
- **THEN** its excerpt and detail page show spans from the stored original text

#### Scenario: Literal matches leave out meaning matches
- **WHEN** a prompt both matches literally and reaches the similarity minimum
- **THEN** it appears only in the literal results
- **THEN** eligible meaning matches for other prompts may still appear under 「意思相近」

#### Scenario: Meaning matches fill an empty literal result
- **WHEN** no prompt is a literal match, one prompt's best score is at or above the configured minimum, and another prompt's best score is below that minimum
- **THEN** only the prompt at or above the minimum appears, under 「意思相近」

#### Scenario: Higher meaning scores come first
- **WHEN** two eligible meaning matches have different scores
- **THEN** the prompt with the higher score appears first under 「意思相近」

#### Scenario: Query embedding failure preserves literal matches
- **WHEN** a query has a literal match and the query embedding is unavailable or fails
- **THEN** the literal match appears in the results
- **THEN** no 「意思相近」 result is shown

#### Scenario: No prompt matches
- **WHEN** the user searches for a substring that no stored text, keyword, or 气质词 contains, and every prompt is either missing vectors or below the configured minimum
- **THEN** the system shows that nothing matched

#### Scenario: Nothing is close enough
- **WHEN** no prompt is a literal match and every prompt is either missing vectors or below the configured minimum
- **THEN** the system shows that nothing matched

#### Scenario: Cap the meaning matches
- **WHEN** literal results include a high-scoring prompt and more other prompts reach the configured minimum than the configured maximum
- **THEN** the system shows only as many nonliteral 「意思相近」 prompts as the configured maximum, keeping the highest scores
