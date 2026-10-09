# prompt-library Specification

## Purpose

让用户把亲手贴入的提示词存进个人库，并为每条提示词保留一个可选的示例链接，以便之后按原文找回，并回看当初为什么收录它。

## Requirements

### Requirement: Store a pasted prompt
The system SHALL store a prompt when the user submits text that contains at least one non-whitespace character. The system SHALL store that text unchanged, including line breaks, surrounding whitespace, and punctuation. The system SHALL NOT collect prompt text from a page, a post, or a URL on the user's behalf.

#### Scenario: Save text without a link
- **WHEN** the user submits prompt text that contains non-whitespace characters and leaves the example URL blank
- **THEN** the system stores one prompt whose text is identical to the submission
- **THEN** that prompt has no example URL

#### Scenario: Reject empty text
- **WHEN** the user submits prompt text that is empty or contains only whitespace
- **THEN** the system does not store a prompt
- **THEN** the system tells the user that the prompt text is required

### Requirement: Keep one optional example URL
The system SHALL keep at most one example URL on a prompt. The system SHALL treat an example URL that is empty or only whitespace as absent. The system SHALL accept an example URL only when it is an absolute `http` or `https` URL with a host. The system SHALL store an accepted URL unchanged apart from trimming surrounding whitespace.

#### Scenario: Save a prompt with one example URL
- **WHEN** the user submits valid prompt text and one absolute http or https example URL
- **THEN** the system stores that URL on the same prompt as the text

#### Scenario: Reject a URL that is not http or https
- **WHEN** the user submits valid prompt text and an example URL that is not an absolute http or https URL with a host
- **THEN** the system does not store a new prompt and does not change an existing prompt
- **THEN** the system tells the user that the example URL must be an http or https link

### Requirement: Open the example URL without fetching it
When a prompt has an example URL, the system SHALL present that URL as a link the user can open in a browser. The system SHALL NOT fetch, preview, download, render, or check reachability of the example URL.

#### Scenario: User opens the stored link
- **WHEN** the user views a prompt that has an example URL
- **THEN** the page offers that URL as a link whose target is the stored URL
- **THEN** the system does not request that URL while saving, listing, searching, or viewing the prompt

#### Scenario: Prompt has no example URL
- **WHEN** the user views a prompt that has no example URL
- **THEN** the page does not present an empty or broken link

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

### Requirement: Read a saved prompt
The system SHALL show the complete stored text when the user opens a prompt, together with its example URL when one is present. The system SHALL tell the user when the requested prompt does not exist.

#### Scenario: Open a stored prompt
- **WHEN** the user opens a prompt that is stored
- **THEN** the page shows the complete stored text, identical to what was saved
- **THEN** the page shows the example URL when the prompt has one

#### Scenario: Open a missing prompt
- **WHEN** the user opens a prompt that is not stored
- **THEN** the system tells the user that the prompt was not found

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

### Requirement: Correct a saved prompt
The system SHALL let the user replace the text and the example URL of an existing prompt, using the same text and URL rules as saving a new prompt. The system SHALL keep the prompt's identity. Clearing the example URL SHALL remove it. The system SHALL NOT create an additional prompt when the user corrects one.

#### Scenario: Replace the text
- **WHEN** the user corrects a stored prompt with new non-empty text
- **THEN** the stored text becomes the corrected text
- **THEN** the prompt keeps the same identity

#### Scenario: Add or clear the example URL
- **WHEN** the user adds an accepted example URL to a prompt that had none, or clears the example URL of a prompt that had one
- **THEN** the prompt's example URL becomes the submitted value, with a blank value stored as absent

#### Scenario: Reject a correction that empties the text
- **WHEN** the user corrects a stored prompt with empty or whitespace-only text
- **THEN** the system keeps the previous text and example URL
- **THEN** the system tells the user that the prompt text is required

### Requirement: Delete a saved prompt
The system SHALL permanently remove a prompt only after the user confirms deletion. Canceling deletion SHALL leave the prompt unchanged. A removed prompt SHALL NOT appear in the list or in search, and opening it SHALL report that it was not found.

#### Scenario: Confirm deletion
- **WHEN** the user confirms deletion of a stored prompt
- **THEN** the prompt is removed from the library
- **THEN** the list, search, and direct open no longer return it

#### Scenario: Cancel deletion
- **WHEN** the user starts deletion and then cancels
- **THEN** the prompt remains stored and unchanged

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

### Requirement: Show when a prompt was added
The system SHALL show the added date on every unfiltered list entry, every literal search result, and every 「意思相近」 result. The added date SHALL be the prompt's creation instant converted to Beijing time (UTC+8) and formatted `YYYY-MM-DD`, preceded by `添加于`. Correcting a prompt SHALL NOT change the shown date. The date SHALL NOT replace the excerpt or the example URL.

#### Scenario: Evening UTC falls on the next Beijing date
- **WHEN** a prompt was created at `2026-09-28T16:00:00Z`
- **THEN** its list entry shows `添加于 2026-09-29`

#### Scenario: Before 16:00 UTC stays on the same Beijing date
- **WHEN** a prompt was created at `2026-09-28T15:59:59Z`
- **THEN** its list entry shows `添加于 2026-09-28`

#### Scenario: Correcting a prompt keeps the added date
- **WHEN** the user corrects an older prompt's text or example URL
- **THEN** that prompt's list entry still shows the date on which it was first saved

#### Scenario: Search results show the added date
- **WHEN** a prompt appears as a literal match or under 「意思相近」
- **THEN** that entry shows the same added date as the unfiltered list

### Requirement: Copy the stored prompt
The system SHALL offer a control labeled `复制` on every unfiltered list entry, every literal search result, every 「意思相近」 result, and the detail page. Activating it SHALL place the complete stored text on the clipboard. The copied text SHALL be identical to the stored text, including line breaks, and SHALL NOT be only the excerpt. The copied text SHALL NOT include the example URL. Markup and scripts in the stored text SHALL be copied as characters and SHALL NOT be executed because the control is present. When the clipboard rejects the text, the control SHALL show `没复制成` and the stored prompt SHALL remain unchanged. After a successful copy, the control SHALL show `已复制`.

#### Scenario: List copies more than the excerpt
- **WHEN** the user activates `复制` on a list entry whose stored text is longer than its excerpt
- **THEN** the copied text is the complete stored text
- **THEN** the copied text does not include the example URL

#### Scenario: Detail copies the stored text
- **WHEN** the user activates `复制` on a stored prompt's detail page
- **THEN** the copied text is identical to the stored text

#### Scenario: A meaning result can be copied
- **WHEN** the user activates `复制` on a 「意思相近」 entry
- **THEN** the copied text is that prompt's complete stored text

#### Scenario: Prompt markup stays literal
- **WHEN** a stored prompt's text contains `<script>alert(1)</script></textarea>` and the page offers `复制`
- **THEN** the list and the detail page show those characters as text
- **THEN** the page does not execute the script

#### Scenario: Clipboard rejection leaves the prompt unchanged
- **WHEN** the user activates `复制` and the clipboard rejects the text
- **THEN** the control shows `没复制成`
- **THEN** the stored prompt is unchanged
