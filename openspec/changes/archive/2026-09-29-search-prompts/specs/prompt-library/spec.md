## MODIFIED Requirements

### Requirement: List saved prompts
The system SHALL list saved prompts with the most recently created prompt first. Correcting a prompt SHALL NOT change its position in that order. Each list entry SHALL show an excerpt of the stored text and SHALL show the example URL when one is present. The excerpt SHALL NOT replace the stored text. On the unfiltered library list, the excerpt SHALL be a leading excerpt of the stored text. On a search result, the excerpt SHALL include the first highlighted match in the stored text. When no prompts are stored, the system SHALL show that the library is empty.

#### Scenario: Newest saved prompt is listed first
- **WHEN** the user has saved two prompts
- **THEN** the prompt that was saved later appears before the prompt that was saved earlier

#### Scenario: Correcting a prompt does not reorder the list
- **WHEN** the user corrects an older prompt's text or example URL
- **THEN** the list order remains the order in which the prompts were first saved

#### Scenario: Empty library
- **WHEN** no prompts are stored
- **THEN** the list tells the user that there are no prompts yet

#### Scenario: Search excerpt includes the first highlight
- **WHEN** a search match occurs after the leading portion of a stored prompt
- **THEN** that prompt's result excerpt includes the highlighted match
- **THEN** the excerpt is not the complete stored text

### Requirement: Search the pasted text
The system SHALL match a query that contains non-whitespace characters by splitting it on whitespace and ignoring empty pieces. A prompt SHALL be a literal match only when every piece occurs as a contiguous substring of that prompt's stored text, of one of its keywords, or of any spelling of one of its 气质词. Latin letters SHALL match regardless of case. Non-Latin text, including Chinese, SHALL match by exact substring. Search SHALL NOT use the example URL, user-managed categories, or user-managed tags as the match corpus. A query that is empty or only whitespace SHALL present the same list as the unfiltered library list. Literal matches SHALL keep the library list order.

When no prompt is a literal match, the system SHALL embed the query as a whole and compare it with that prompt's section vectors, keyword vectors, and 气质词 vectors. The prompt's score SHALL be the highest of those similarities. The system SHALL show prompts whose score is at or above the configured minimum, at most the configured maximum, under the label 「意思相近」, ordered from highest score to lowest. The system SHALL NOT show 「意思相近」 when any literal match exists. When no literal match exists and no prompt both has vectors and reaches the minimum, the system SHALL show that nothing matched.

#### Scenario: Match a Chinese substring
- **WHEN** a stored prompt's text contains `剪纸` and the user searches for `剪纸`
- **THEN** that prompt appears in the results

#### Scenario: Match Latin letters regardless of case
- **WHEN** a stored prompt's text contains `Paper-cut` and the user searches for `paper-cut`
- **THEN** that prompt appears in the results

#### Scenario: Match every space-separated piece
- **WHEN** one stored prompt's text contains `垂直构图` and `海滩` in different places, another stored prompt's text contains only `海滩`, and the user searches for `垂直 海滩`
- **THEN** the first prompt appears in the literal results
- **THEN** the second prompt is absent from the literal results

#### Scenario: Match a grouped spelling
- **WHEN** a prompt has a 气质词 whose text span is `剪纸风格` and whose grouped spelling includes `paper-cut`, and the user searches for `paper-cut`
- **THEN** that prompt appears in the literal results

#### Scenario: Do not match the example URL alone
- **WHEN** the query occurs only in a prompt's example URL and does not occur in its text, keywords, or 气质词
- **THEN** that prompt is absent from the results

#### Scenario: Blank search lists the library
- **WHEN** the user searches with an empty or whitespace-only query
- **THEN** the system shows the same prompts in the same order as the unfiltered list

#### Scenario: Literal matches leave out meaning matches
- **WHEN** at least one prompt is a literal match
- **THEN** the results do not include 「意思相近」

#### Scenario: Meaning matches fill an empty literal result
- **WHEN** no prompt is a literal match, one prompt's best score is at or above the configured minimum, and another prompt's best score is below that minimum
- **THEN** only the prompt at or above the minimum appears, under 「意思相近」

#### Scenario: Higher meaning scores come first
- **WHEN** no prompt is a literal match and two prompts both reach the configured minimum
- **THEN** the prompt with the higher score appears before the prompt with the lower score

#### Scenario: No prompt matches
- **WHEN** the user searches for a substring that no stored text, keyword, or 气质词 contains, and every prompt is either missing vectors or below the configured minimum
- **THEN** the system shows that nothing matched

#### Scenario: Nothing is close enough
- **WHEN** no prompt is a literal match and every prompt is either missing vectors or below the configured minimum
- **THEN** the system shows that nothing matched

#### Scenario: Cap the meaning matches
- **WHEN** no prompt is a literal match and more prompts reach the configured minimum than the configured maximum
- **THEN** the system shows only as many 「意思相近」 prompts as the configured maximum, keeping the highest scores

### Requirement: Show prompt text literally
The system SHALL present stored prompt text as literal text. Markup, scripts, and markdown in the text SHALL be visible as characters and SHALL NOT be executed or interpreted as page structure. A search highlight SHALL wrap characters that are already in the stored text and SHALL NOT cause those characters to be executed or interpreted as page structure.

#### Scenario: Prompt contains HTML
- **WHEN** a stored prompt's text contains `<script>alert(1)</script>`
- **THEN** the list and the prompt page show those characters as text
- **THEN** the system does not execute the script

#### Scenario: Highlighted HTML stays literal
- **WHEN** a search highlights a match inside a stored prompt whose text contains `<script>alert(1)</script>`
- **THEN** the page shows those characters as text
- **THEN** the system does not execute the script

## ADDED Requirements

### Requirement: Highlight the matched passage
The system SHALL highlight search matches inside the stored text. A literal match SHALL highlight every occurrence of each matched piece in the text. When a piece matches only a grouped spelling, the system SHALL highlight the 气质词's text span instead of the grouped spelling. A 「意思相近」 match SHALL highlight only the highest-scoring section, or the text span of the highest-scoring keyword or 气质词. The system SHALL NOT add text in order to highlight a term that has no span in the stored text. Opening a prompt from a search result SHALL show the same highlights in the complete text. Opening a prompt without a search query SHALL NOT highlight the text.

#### Scenario: Literal highlights mark every occurrence
- **WHEN** the user searches for `海滩` and a matching prompt contains `海滩` twice
- **THEN** the complete text highlights both occurrences

#### Scenario: Grouped spelling highlights the original span
- **WHEN** the user searches for `paper-cut` and that spelling is grouped with a 气质词 whose text span is `剪纸风格`
- **THEN** the complete text highlights `剪纸风格`
- **THEN** the page does not insert `paper-cut` into the stored text

#### Scenario: Meaning highlight marks the winning section or term
- **WHEN** a prompt appears under 「意思相近」 because one section scored higher than its other vectors
- **THEN** the complete text highlights that section
- **THEN** the complete text does not highlight the lower-scoring sections

#### Scenario: Opening from the library does not highlight
- **WHEN** the user opens a stored prompt from the unfiltered library list
- **THEN** the complete text has no search highlight
