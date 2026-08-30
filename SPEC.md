# discord-retriever — Specification

## Overview

A Go CLI utility with three commands:
1. **get-messages** — uses a Discord **user token** (not a bot token) to download all messages and attachments from a single channel. Each message is stored as a unique markdown file named `<messageId>.md`.
2. **review** — passes downloaded messages through an LLM to extract structured notes (People, Places, Events) into an Obsidian-compatible vault with frontmatter, fuzzy deduplication, and backlink propagation.
3. **missed** — scans a channel for a user's unclaimed adventure posts and prints those whose in-message date is past a start date.

---

## CLI Interface

```
discord-retriever <command> [flags]

Commands:
  get-messages    Fetch and save messages from a Discord channel
  review          Extract structured notes from downloaded messages via LLM
  missed          List unclaimed adventures after a start date from a channel
```

### get-messages

```
discord-retriever get-messages [flags]

Flags:
  --token    string   User token (or DISCORD_TOKEN env var)
  --channel  string   Channel ID (or DISCORD_CHANNEL_ID env var)
  --output   string   Output directory (default: ./output)
  --verbose           Print progress to stderr
```

### review

```
discord-retriever review [flags]

Flags:
  --api-key  string   LLM API key (or OPENAI_API_KEY env var)
  --base-url string   LLM API base URL (or OPENAI_BASE_URL env var)
  --model    string   LLM model name (or LLM_MODEL env var)
  --input    string   Directory with raw message markdown files (default: ./output)
  --notes    string   Obsidian vault / notes output directory (default: ./notes)
  --files    string   Comma-separated filenames to process (omit to process all .md files)
  --verbose           Print progress to stderr
```

---

### missed

```
discord-retriever missed [flags]

Flags:
  --token      string   User token (or DISCORD_TOKEN env var)
  --channel    string   Channel ID
  --start-date string   Start date (YYYY-MM-DD)
  --user-id    string   User ID to scan messages for
  --verbose             Print progress to stderr
```

Scans every message in the channel and keeps those that match all of the following:

- Message author ID equals `--user-id`
- Content contains the exact markdown `### **GM:** *Waiting for a GM to claim this adventure*`
- The `<t:<epoch>:F>` timestamp embedded in the message content resolves to a date strictly after `--start-date` and not after the current time

For each match it prints to stdout, one line per match:

```
<Session Name> <Expected Date> <Link to Sign Up>
```

- **Session Name** — the text after the leading `# ` heading in the message content
- **Expected Date** — the `<t:<epoch>:F>` timestamp rendered as `YYYY-MM-DD` (UTC)
- **Link to Sign Up** — the URL of the button component labeled `View Details & Sign Up`

If the session name starts with `[DELETED]` (the bot's marker for a deleted announcement), the message is still tracked without a sign-up button, and the link is rendered as the literal text `DELETED`.

Messages missing any required piece (no timestamp, no heading, no sign-up button for non-deleted sessions) are silently skipped.

---

## Project Structure

```
discord-retriever/
├── go.mod
├── Makefile
├── main.go
├── internal/
│   ├── discord/
│   │   └── client.go       # Discord HTTP client, pagination, rate-limit handling
│   ├── missed/
│   │   └── missed.go       # unclaimed-adventure scan + print (missed command)
│   ├── writer/
│   │   └── writer.go       # markdown rendering + attachment download
│   ├── llm/
│   │   └── client.go        # OpenAI-compatible chat completions client
│   ├── notes/
│   │   ├── store.go         # Obsidian vault file management
│   │   └── store_test.go    # unit tests for matching and frontmatter
│   └── review/
│       └── reviewer.go      # three-pass LLM review pipeline
└── output/                  # default get-messages output directory
```

---

## Authentication

### Discord (get-messages)
User tokens are sent as a bare `Authorization: <token>` header (no `Bot ` prefix). This matches how the Discord web client authenticates. The tool accepts the token via `--token` flag or `DISCORD_TOKEN` environment variable.

### LLM (review)
The review command requires an OpenAI-compatible API. Credentials are accepted via flags or environment variables:
- `--api-key` / `OPENAI_API_KEY`
- `--base-url` / `OPENAI_BASE_URL`
- `--model` / `LLM_MODEL`

---

## get-messages

### Pagination Strategy

- Fetch messages in batches of 100 using `GET /api/v10/channels/{id}/messages?limit=100&before=<lastSnowflakeID>`
- Walk backwards through the channel (newest to oldest) until an empty response is returned
- Collect all message IDs, then reverse to chronological order before writing
- Skip messages where `<messageId>.md` already exists in the output directory (resume support)

### Output Directory Structure

```
<output>/
├── <messageId>.md
├── <messageId>.md
└── attachments/
    └── <messageId>_<original_filename>
```

Snowflake IDs are time-ordered, so alphabetical sort of filenames equals chronological order.

### Markdown Format

```markdown
# <messageId>

**Author**: username (<userId>)
**Timestamp**: 2024-01-15T10:30:00Z
**Channel**: <channelId>

---

<message content>

## Replies
**Replying to**: <referencedMessageId>

## Reactions
- 👍 × 5
- ❤️ × 2

## Embeds

### <embed title>
**URL**: https://...
**Description**: ...
**Author**: ...
**Footer**: ...

## Attachments
- [filename.png](./attachments/<messageId>_filename.png)
```

Sections that have no content (no reactions, no embeds, no attachments, no reply) are omitted entirely.

### Rate Limit Handling

- Check `X-RateLimit-Remaining` on every response; sleep 500ms when ≤1 remaining
- On `429`: read `Retry-After` header, sleep that duration, retry (up to 3 retries)
- Add a small courtesy delay (50ms) between paginated requests to avoid hammering the API

### Attachment Download

- Attachments saved to `<output>/attachments/<messageId>_<original_filename>`
- Referenced in the markdown via relative path: `./attachments/<messageId>_<filename>`
- On download failure: log a warning to stderr and write the markdown with `(download failed)` appended next to the attachment link; continue processing
- Existing attachments are skipped on resume

---

## review

### Overview

The review command runs a three-pass pipeline over the raw message markdown files:

1. **Pass 1: Extraction** — for each message, send the markdown to an LLM and ask it to extract structured notes (People, Places, Events). Fuzzy-match note titles against existing notes; merge updates into existing notes or create new ones.
2. **Pass 2: Cleaning** — for every note that received updates, send it to the LLM to merge appended "UPDATED INFORMATION" sections into the main body naturally and add `[[wiki links]]` for known entities.
3. **Pass 3: Backlink Propagation** — scan all untouched notes; if they mention a title of a new/updated note as plain text, ask the LLM to wrap those mentions in `[[wiki links]]`.

### Obsidian Vault Structure

```
<notesDir>/
├── people/
│   ├── John Smith.md
│   └── Jane Doe.md
├── places/
│   └── The Hungry Dragon Inn.md
└── events/
    └── Battle of Darkwood.md
```

### Note YAML Frontmatter

Each generated note includes:

```yaml
---
title: "Note Title"
category: People
tags: [people, adventurer, innkeeper]
source_messages:
  - "1281429034735108178"
  - "1404879949135085578"
first_seen: "2024-09-06"
discord_username: "johnsmith"    # optional, only for People
---
```

### Note Deduplication (Fuzzy Matching)

When extracting notes, titles are compared against existing notes in the same category using word-set overlap matching:
- One title's normalized word set must be a subset of the other's
- The Jaccard overlap score is used to pick the best candidate among multiple matches
- If a place name matches an existing person, it is reclassified to People

### LLM Prompting

#### Pass 1 — Extraction

The LLM is asked to return **only a JSON array** of objects with:
- `title` — entity name
- `category` — exactly one of `"People"`, `"Places"`, `"Events"`
- `tags` — array of lowercase tags
- `content` — full markdown body (no frontmatter)
- `metadata` — optional object (e.g. `{"discord_username": "username"}`)

Guidelines given to the LLM:
- For People: extract a `discord_username` from the Author line first, then fall back to @mentions
- For Events: include an "Attendees" section listing people who attended or were mentioned
- For Places: if the location name matches a known person's name, classify it as People

#### Pass 2 — Cleaning

The LLM is asked to merge "UPDATED INFORMATION" sections into the main body, add `[[wiki links]]` for known entities, keep YAML frontmatter intact, and return only the cleaned markdown.

#### Pass 3 — Backlinks

The LLM is asked to wrap specific names in `[[wiki links]]` wherever they appear as plain text, without changing anything else.

### LLM JSON Sanitization

Before parsing the LLM response:
1. Extract JSON from markdown code fences (handles ` ```json ... ``` `)
2. Sanitize common LLM JSON mistakes:
   - Replace Python-style `\'` escapes with literal `'` (JSON does not allow `\'`)
   - Strip trailing commas before `]` and `}`
3. Unmarshal into Go structs with `map[string]any` for the `metadata` field to tolerate booleans, arrays, and nested objects

### Source-Message Deduplication

Each note tracks the Discord message IDs that contributed to it in `source_messages`. When re-processing a message, if the message ID is already present in a matching note's frontmatter, the update is skipped. This makes the review command idempotent and safe to re-run.

---

## Resume Behavior

### get-messages
Before writing a message, check if `<output>/<messageId>.md` already exists. If it does, skip that message entirely (including re-downloading its attachments). In verbose mode, log the count of skipped messages at the end.

### review
Re-running review on the same input is safe:
- Existing notes with the same source message ID are skipped
- New or updated notes go through all three passes
- Backlinks are only added to notes that were *not* updated in Pass 1, avoiding redundant work

---

## Unresolved / Out of Scope

- No support for downloading messages from multiple channels in one invocation
- No support for threads (only the top-level channel messages)
- No support for stickers or voice messages beyond noting their presence
- No support for DMs or group DMs
- Backlink propagation only adds links; it never removes stale links
