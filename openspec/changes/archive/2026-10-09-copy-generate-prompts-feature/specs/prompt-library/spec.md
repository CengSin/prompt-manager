## ADDED Requirements

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
