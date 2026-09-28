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
The system SHALL list saved prompts with the most recently created prompt first. Correcting a prompt SHALL NOT change its position in that order. Each list entry SHALL show a leading excerpt of the stored text and SHALL show the example URL when one is present. The excerpt SHALL NOT replace the stored text. When no prompts are stored, the system SHALL show that the library is empty.

#### Scenario: Newest saved prompt is listed first
- **WHEN** the user has saved two prompts
- **THEN** the prompt that was saved later appears before the prompt that was saved earlier

#### Scenario: Correcting a prompt does not reorder the list
- **WHEN** the user corrects an older prompt's text or example URL
- **THEN** the list order remains the order in which the prompts were first saved

#### Scenario: Empty library
- **WHEN** no prompts are stored
- **THEN** the list tells the user that there are no prompts yet

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
The system SHALL return prompts whose stored text contains the query as a contiguous substring. Latin letters SHALL match regardless of case. Non-Latin text, including Chinese, SHALL match by exact substring. Search SHALL NOT use categories, tags, or the example URL as the match corpus. A query that is empty or only whitespace SHALL present the same list as the unfiltered library list. Matching prompts SHALL keep the library list order.

#### Scenario: Match a Chinese substring
- **WHEN** a stored prompt's text contains `剪纸` and the user searches for `剪纸`
- **THEN** that prompt appears in the results

#### Scenario: Match Latin letters regardless of case
- **WHEN** a stored prompt's text contains `Paper-cut` and the user searches for `paper-cut`
- **THEN** that prompt appears in the results

#### Scenario: Do not match the example URL alone
- **WHEN** the query occurs only in a prompt's example URL and does not occur in its text
- **THEN** that prompt is absent from the results

#### Scenario: No prompt matches
- **WHEN** the user searches for a substring that no stored text contains
- **THEN** the system shows that nothing matched

#### Scenario: Blank search lists the library
- **WHEN** the user searches with an empty or whitespace-only query
- **THEN** the system shows the same prompts in the same order as the unfiltered list

### Requirement: Show prompt text literally
The system SHALL present stored prompt text as literal text. Markup, scripts, and markdown in the text SHALL be visible as characters and SHALL NOT be executed or interpreted as page structure.

#### Scenario: Prompt contains HTML
- **WHEN** a stored prompt's text contains `<script>alert(1)</script>`
- **THEN** the list and the prompt page show those characters as text
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
