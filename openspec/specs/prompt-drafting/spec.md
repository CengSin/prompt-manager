# prompt-drafting Specification

## Purpose

让用户用一段自己的需求生成一份新的提示词草稿。草稿参照库里已有提示词的写法，题材以这次输入为准，并在用户保存之前不进入提示词库。

## Requirements

### Requirement: Ask what to write
The system SHALL offer `写一条` from the library. That page SHALL accept a description labeled `要写的内容` and SHALL NOT store the description. Opening the page SHALL NOT store a prompt. Submitting a description that is empty or only whitespace SHALL NOT call the writing model, SHALL NOT store a prompt, and SHALL tell the user `要写的内容必填`.

#### Scenario: The library opens drafting
- **WHEN** the user views the library
- **THEN** the page offers `写一条`
- **THEN** opening it does not store a prompt

#### Scenario: Reject an empty description
- **WHEN** the user submits a description that is empty or only whitespace
- **THEN** the system does not call the writing model
- **THEN** no prompt is stored
- **THEN** the page shows `要写的内容必填` and still shows what the user submitted

### Requirement: Select writing examples
When the user submits a description that contains non-whitespace characters, the system SHALL embed that description as a whole. A saved prompt's score SHALL be the highest similarity among its section vectors, keyword vectors, and 气质词 vectors for the current stored text. The system SHALL select at most 3 prompts whose score is at or above the configured ordinary similarity minimum, ordered from highest score to lowest. The system SHALL NOT apply the higher minimum used for short search queries. A prompt SHALL remain eligible when its stored text, a keyword, or a 气质词 spelling also contains the description as a literal match. A prompt with no vectors for its current text SHALL NOT be selected. Each selected example SHALL contribute its complete stored text and SHALL NOT contribute its example URL. When embeddings are not configured, the embedding request fails, or no prompt reaches the minimum, the system SHALL continue with no examples.

#### Scenario: The three closest prompts are selected
- **WHEN** four prompts have vectors and only three score at or above the ordinary similarity minimum
- **THEN** the writing request includes the stored text of those three, highest score first
- **THEN** the writing request does not include the prompt below the minimum

#### Scenario: A literal match remains eligible
- **WHEN** the highest-scoring prompt also contains the description as a contiguous substring
- **THEN** the writing request still includes that prompt's stored text

#### Scenario: A short description uses the ordinary minimum
- **WHEN** the description is four characters or fewer and a prompt scores at or above the ordinary similarity minimum but below the short-query minimum
- **THEN** that prompt is eligible as an example

#### Scenario: More than three matches keep the highest
- **WHEN** five prompts score at or above the ordinary similarity minimum
- **THEN** the writing request includes only the three with the highest scores

#### Scenario: Missing vectors are skipped
- **WHEN** one prompt has no vectors for its current text and another prompt reaches the ordinary similarity minimum
- **THEN** the writing request includes only the prompt that has vectors

#### Scenario: The example URL is not sent
- **WHEN** a selected prompt has an example URL
- **THEN** the writing request includes that prompt's stored text
- **THEN** the writing request does not include the example URL

#### Scenario: No eligible example still continues
- **WHEN** every prompt is missing vectors or below the ordinary similarity minimum
- **THEN** the system still requests a draft
- **THEN** the writing request does not include a stored prompt as an example

### Requirement: Draft into the paste form
The writing request SHALL name the user's description as the subject of the prompt to write, and SHALL present any selected stored texts only as samples of how to write. The system SHALL read the draft from the completion message content and SHALL trim only its surrounding whitespace, keeping internal line breaks. When that text contains a non-whitespace character, the system SHALL show it in the existing new-prompt form, with the example URL empty. The form SHALL show `这是生成的草稿，改完再保存`. When the draft was made with no examples, the form SHALL also show `这次没有参考`. The system SHALL NOT store a prompt, the description, or a generated marker because the draft was shown. Submitting that form SHALL follow the existing rules for pasting a new prompt. Leaving the form without submitting SHALL leave the library unchanged.

#### Scenario: A draft opens unsaved
- **WHEN** the writing model returns a non-empty prompt
- **THEN** the new-prompt form shows that text in the prompt field
- **THEN** the example URL field is empty
- **THEN** the page shows `这是生成的草稿，改完再保存`
- **THEN** no prompt is stored yet

#### Scenario: The description is the subject
- **WHEN** the user submits a description and one or more examples are selected
- **THEN** the writing request identifies the description as what the new prompt is about
- **THEN** the writing request identifies the selected texts as writing samples

#### Scenario: No examples are announced
- **WHEN** a draft is shown and no writing example was selected
- **THEN** the new-prompt form shows `这次没有参考`

#### Scenario: Saving the edited draft stores one prompt
- **WHEN** the user edits the draft and submits the new-prompt form with valid text
- **THEN** the system stores one prompt whose text is identical to that submission
- **THEN** the system does not store the original description

#### Scenario: Leaving the draft stores nothing
- **WHEN** the user opens a generated draft and leaves without submitting the new-prompt form
- **THEN** the library does not gain a prompt from that draft

### Requirement: Refuse an unusable draft
When the writing endpoint has no API key or no model, the system SHALL NOT send a writing request, SHALL NOT store a prompt, and SHALL tell the user `还不能生成`. When the writing request fails, or the completion is empty or only whitespace, the system SHALL NOT store a prompt, SHALL stay on the drafting page, SHALL keep the submitted description, and SHALL tell the user `没有生成出来`. Text shown for that failure SHALL NOT contain an API key.

#### Scenario: Drafting is not configured
- **WHEN** the writing endpoint has no API key or no model and the user submits a description
- **THEN** the system does not send a writing request
- **THEN** no prompt is stored
- **THEN** the page shows `还不能生成`

#### Scenario: A failed draft keeps the description
- **WHEN** the writing request fails or returns text that is empty or only whitespace
- **THEN** no prompt is stored
- **THEN** the drafting page shows the submitted description and `没有生成出来`

#### Scenario: A failure does not reveal the API key
- **WHEN** the writing request fails with a response that contains the writing API key
- **THEN** the page does not show that API key
