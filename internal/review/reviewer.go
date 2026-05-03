package review

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"discord-retriever/internal/llm"
	"discord-retriever/internal/notes"
)

// Config drives the review command.
type Config struct {
	InputDir string
	NotesDir string
	Verbose  bool
	LLM      *llm.Client
}

// Run executes the three-pass review pipeline.
func Run(cfg Config) error {
	store := notes.NewStore(cfg.NotesDir, cfg.Verbose)

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Starting Pass 1: extracting notes from %s...\n", cfg.InputDir)
	}

	updatedFiles := make(map[string]bool)
	newOrUpdatedTitles := make(map[string]bool)

	entries, err := os.ReadDir(cfg.InputDir)
	if err != nil {
		return fmt.Errorf("failed to read input directory: %w", err)
	}

	processedCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		inputPath := filepath.Join(cfg.InputDir, entry.Name())
		slog.Info("processing file", "filename", entry.Name())
		if err := processSourceFile(inputPath, cfg, store, updatedFiles, newOrUpdatedTitles); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to process %s: %v\n", entry.Name(), err)
			continue
		}
		processedCount++
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Pass 1 complete. Processed %d files, %d updated.\n", processedCount, len(updatedFiles))
		fmt.Fprintf(os.Stderr, "Starting Pass 2: cleaning updated files...\n")
	}

	// Pass 2: clean notes that received updates
	allTitles, err := store.AllTitles()
	if err != nil {
		return fmt.Errorf("failed to list all titles: %w", err)
	}

	for path := range updatedFiles {
		if err := cleanNote(path, cfg.LLM, allTitles); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean %s: %v\n", path, err)
		}
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Pass 2 complete.\n")
		fmt.Fprintf(os.Stderr, "Starting Pass 3: backlink propagation...\n")
	}

	// Pass 3: add backlinks to other notes that mention new/updated entities
	allFiles, err := store.AllFiles()
	if err != nil {
		return fmt.Errorf("failed to list all files: %w", err)
	}

	backlinkedCount := 0
	for _, path := range allFiles {
		if updatedFiles[path] {
			continue
		}

		content, err := store.ReadFile(path)
		if err != nil {
			continue
		}

		titlesToLink := []string{}
		for title := range newOrUpdatedTitles {
			if containsUnlinkedTitle(content, title) {
				titlesToLink = append(titlesToLink, title)
			}
		}

		if len(titlesToLink) == 0 {
			continue
		}

		if err := addBacklinks(path, content, titlesToLink, cfg.LLM, store); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to add backlinks to %s: %v\n", path, err)
			continue
		}
		backlinkedCount++
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Pass 3 complete. Updated %d files with backlinks.\n", backlinkedCount)
	}

	return nil
}

// ------------------------------------------------------------------
// Pass 1 helpers
// ------------------------------------------------------------------

type extractedNote struct {
	Title    string            `json:"title"`
	Category string            `json:"category"`
	Tags     []string          `json:"tags"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
}

func processSourceFile(inputPath string, cfg Config, store *notes.Store, updatedFiles map[string]bool, newOrUpdatedTitles map[string]bool) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	content := string(data)

	// pull message ID from the first line: "# 1281429034735108178"
	msgID := ""
	lines := strings.Split(content, "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		if strings.HasPrefix(firstLine, "# ") {
			msgID = strings.TrimPrefix(firstLine, "# ")
		}
	}

	// pull timestamp for first_seen date
	timestamp := ""
	for _, line := range lines {
		if strings.Contains(line, "**Timestamp**:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				timestamp = strings.TrimSpace(parts[1])
				break
			}
		}
	}

	firstSeen := ""
	if timestamp != "" {
		if idx := strings.Index(timestamp, "T"); idx > 0 {
			firstSeen = timestamp[:idx]
		} else {
			firstSeen = timestamp
		}
	}

	systemPrompt := `You are an Obsidian knowledge base assistant. Given a Discord message (with metadata, reactions, attachments, etc.), extract structured notes for any notable People, Places, or Events mentioned.

Return ONLY a JSON array. Empty array [] if nothing notable.
Each object must have:
- title: the entity name
- category: exactly one of "People", "Places", "Events"
- tags: array of relevant lowercase tags
- content: full markdown body (no frontmatter)
- metadata: optional object with additional info (e.g. {"discord_username": "username"})

Guidelines:
- For People: try to extract a discord_username from the Author line first, then fall back to @mentions. Include it in the metadata object.
- For Events: include an "Attendees" section in the content listing people who attended or were mentioned.
- For Places: if the location name matches a known person's name, classify it as People instead of Places.

Example:
[
  {
    "title": "John Smith",
    "category": "People",
    "tags": ["people", "adventurer", "innkeeper"],
    "content": "John Smith is the innkeeper at the Hungry Dragon Inn. He is known for offering jobs to adventurers.",
    "metadata": {"discord_username": "johnsmith"}
  },
  {
    "title": "Battle of Darkwood",
    "category": "Events",
    "tags": ["events", "battle", "war"],
    "content": "The Battle of Darkwood was fought on March 15th.\\n\\n**Attendees:**\\n- John Smith\\n- Captain Vance\\n- Lady Elara",
    "metadata": {}
  }
]`

	llmResponse, err := cfg.LLM.Chat(systemPrompt, content)
	if err != nil {
		return fmt.Errorf("LLM chat failed: %w", err)
	}

	jsonStr := extractJSON(llmResponse)
	if jsonStr == "" {
		jsonStr = llmResponse
	}

	var extractedNotes []extractedNote
	if err := json.Unmarshal([]byte(jsonStr), &extractedNotes); err != nil {
		return fmt.Errorf("failed to parse LLM response: %w\nResponse: %s", err, llmResponse)
	}

	for _, note := range extractedNotes {
		if note.Title == "" || note.Content == "" {
			continue
		}

		category := normalizeCategory(note.Category)
		if category == "" {
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  Skipping note with unknown category %q: %s\n", note.Category, note.Title)
			}
			continue
		}

		var matchPath string
		var found bool

		if category == "Places" {
			if peoplePath, peopleFound := store.FindMatch(note.Title, "People"); peopleFound {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "  Location %q matches existing person, reclassifying to People\n", note.Title)
				}
				category = "People"
				matchPath = peoplePath
				found = true
			}
		}

		if !found {
			matchPath, found = store.FindMatch(note.Title, category)
		}

		discordUsername := ""
		if category == "People" && note.Metadata != nil {
			discordUsername = note.Metadata["discord_username"]
		}

		if found {
			if store.HasSourceMessage(matchPath, msgID) {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "  Skipping duplicate source %s for %s\n", msgID, note.Title)
				}
				continue
			}
			if err := store.AppendUpdate(matchPath, filepath.Base(inputPath), note.Content); err != nil {
				return fmt.Errorf("failed to append update to %s: %w", matchPath, err)
			}
			updatedFiles[matchPath] = true
			newOrUpdatedTitles[note.Title] = true

			if discordUsername != "" {
				if err := store.SetFrontmatterField(matchPath, "discord_username", discordUsername); err != nil {
					if cfg.Verbose {
						fmt.Fprintf(os.Stderr, "  Warning: failed to set discord_username: %v\n", err)
					}
				}
			}

			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  Updated: %s -> %s\n", note.Title, matchPath)
			}
		} else {
			if err := store.WriteNew(note.Title, category, note.Content, firstSeen, msgID, note.Tags, discordUsername); err != nil {
				return fmt.Errorf("failed to write new note: %w", err)
			}
			newOrUpdatedTitles[note.Title] = true
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  Created: %s (%s)\n", note.Title, category)
			}
		}
	}

	return nil
}

func normalizeCategory(cat string) string {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "people", "person":
		return "People"
	case "places", "place", "location":
		return "Places"
	case "events", "event":
		return "Events"
	default:
		return ""
	}
}

// extractJSON tries to pull a JSON object/array out of a markdown code fence.
func extractJSON(s string) string {
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + 3
		if end := strings.Index(s[start:], "```"); end >= 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if strings.HasPrefix(candidate, "[") || strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}
	return ""
}

// ------------------------------------------------------------------
// Pass 2 helpers
// ------------------------------------------------------------------

func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx > 0 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "\n```") {
		if idx := strings.LastIndex(s, "\n```"); idx > 0 {
			s = s[:idx]
		}
	} else if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func cleanNote(path string, client *llm.Client, allTitles []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	titlesList := strings.Join(allTitles, ", ")

	systemPrompt := fmt.Sprintf(`You are an Obsidian knowledge base editor. The following note has one or more "UPDATED INFORMATION" sections appended to it.

Clean it up by:
1. Merging the updated information naturally into the main body
2. Adding [[wiki links]] for any of the following known entities when mentioned as plain text: %s
3. Removing the "UPDATED INFORMATION" section headers once merged
4. Keeping the YAML frontmatter intact; update tags if warranted based on merged content
5. Return ONLY the complete cleaned markdown file contents. No commentary, no markdown code block wrappers.`, titlesList)

	cleaned, err := client.Chat(systemPrompt, content)
	if err != nil {
		return err
	}

	cleaned = stripCodeBlock(cleaned)
	return os.WriteFile(path, []byte(cleaned+"\n"), 0644)
}

// ------------------------------------------------------------------
// Pass 3 helpers
// ------------------------------------------------------------------

var wikiLinkRegex = regexp.MustCompile(`\[\[.*?\]\]`)

func containsUnlinkedTitle(content, title string) bool {
	plainText := wikiLinkRegex.ReplaceAllString(content, " ")
	lowerPlain := strings.ToLower(plainText)
	lowerTitle := strings.ToLower(title)

	// exact substring
	if strings.Contains(lowerPlain, lowerTitle) {
		return true
	}

	titleWords := strings.Fields(normalizeForMatch(title))
	if len(titleWords) <= 1 {
		return false
	}

	plainWords := make(map[string]bool)
	for _, w := range strings.Fields(normalizeForMatch(plainText)) {
		plainWords[w] = true
	}

	for _, w := range titleWords {
		if !plainWords[w] {
			return false
		}
	}
	return true
}

func normalizeForMatch(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func addBacklinks(path, content string, titlesToLink []string, client *llm.Client, store *notes.Store) error {
	titlesList := strings.Join(titlesToLink, ", ")

	systemPrompt := fmt.Sprintf(`You are an Obsidian knowledge base editor. Update the following note by wrapping these specific names in [[wiki links]] wherever they appear as plain text (not already linked): %s

Rules:
- Only add links for the names listed above
- Do not change anything else
- Return ONLY the complete markdown file contents. No commentary, no markdown code block wrappers.`, titlesList)

	updated, err := client.Chat(systemPrompt, content)
	if err != nil {
		return err
	}

	updated = stripCodeBlock(updated)
	return store.WriteFile(path, updated+"\n")
}
