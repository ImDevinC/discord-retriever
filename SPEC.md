# discord-retriever — Specification

## Overview

A single-command Go CLI utility that uses a **Discord user token** (not a bot token) to download all messages and attachments from a single channel. Each message is stored as a unique markdown file named `<messageId>.md`.

---

## CLI Interface

```
discord-retriever [flags]

Flags:
  --token    string   User token (or DISCORD_TOKEN env var)
  --channel  string   Channel ID (or DISCORD_CHANNEL_ID env var)
  --output   string   Output directory (default: ./output)
  --verbose           Print progress to stderr
```

---

## Project Structure

```
discord-retriever/
├── go.mod
├── Makefile
├── main.go
├── discord/
│   └── client.go      # HTTP client, pagination, rate-limit handling
└── output/
    └── writer.go      # markdown rendering + attachment download
```

---

## Authentication

User tokens are sent as a bare `Authorization: <token>` header (no `Bot ` prefix). This matches how the Discord web client authenticates. The tool accepts the token via `--token` flag or `DISCORD_TOKEN` environment variable.

---

## Pagination Strategy

- Fetch messages in batches of 100 using `GET /api/v10/channels/{id}/messages?limit=100&before=<lastSnowflakeID>`
- Walk backwards through the channel (newest to oldest) until an empty response is returned
- Collect all message IDs, then reverse to chronological order before writing
- Skip messages where `<messageId>.md` already exists in the output directory (resume support)

---

## Output Directory Structure

```
<output>/
├── <messageId>.md
├── <messageId>.md
└── attachments/
    └── <messageId>_<original_filename>
```

Snowflake IDs are time-ordered, so alphabetical sort of filenames equals chronological order.

---

## Markdown Format

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

## Attachments
- [filename.png](./attachments/<messageId>_filename.png)
```

Sections that have no content (no reactions, no embeds, no attachments, no reply) are omitted entirely.

---

## Rate Limit Handling

- Check `X-RateLimit-Remaining` on every response
- On `429`: read `Retry-After` header, sleep that duration, retry (up to 3 retries)
- Add a small courtesy delay (50ms) between paginated requests to avoid hammering the API

---

## Attachment Download

- Attachments saved to `<output>/attachments/<messageId>_<original_filename>`
- Referenced in the markdown via relative path: `./attachments/<messageId>_<filename>`
- On download failure: log a warning to stderr and write the markdown with `(download failed)` appended next to the attachment link; continue processing

---

## Resume Behavior

- Before writing a message, check if `<output>/<messageId>.md` already exists
- If it does, skip that message entirely (including re-downloading its attachments)
- In verbose mode, log the count of skipped messages at the end

---

## Embed Metadata in Markdown

Include embed content in the markdown output:
- Embed title (if present)
- URL (if present)
- Description (if present)
- Author name (if present)
- Footer text (if present)

---

## Unresolved / Out of Scope

- No support for downloading messages from multiple channels in one invocation
- No support for threads (only the top-level channel messages)
- No support for stickers or voice messages beyond noting their presence
