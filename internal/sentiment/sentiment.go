package sentiment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"discord-retriever/internal/llm"
)

type Config struct {
	InputDir string
	Verbose  bool
	LLM      *llm.Client
	Files    []string
}

func Run(cfg Config) error {
	entries, err := os.ReadDir(cfg.InputDir)
	if err != nil {
		return fmt.Errorf("failed to read input directory: %w", err)
	}

	filesFilter := make(map[string]bool, len(cfg.Files))
	for _, f := range cfg.Files {
		filesFilter[f] = true
	}

	processedCount := 0
	skippedCount := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if len(filesFilter) > 0 && !filesFilter[entry.Name()] {
			continue
		}

		path := filepath.Join(cfg.InputDir, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read %s: %v\n", entry.Name(), err)
			continue
		}
		content := string(data)

		if hasCharacterField(content) {
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  Skipping %s (character already set)\n", entry.Name())
			}
			skippedCount++
			continue
		}

		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "  Processing %s...\n", entry.Name())
		}

		characterName, err := determineCharacter(cfg.LLM, content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to determine character for %s: %v\n", entry.Name(), err)
			continue
		}

		if characterName == "" || characterName == "Unknown" {
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  Could not determine character for %s, skipping\n", entry.Name())
			}
			continue
		}

		updated := insertCharacterFrontmatter(content, characterName)
		if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write %s: %v\n", entry.Name(), err)
			continue
		}

		processedCount++
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "  Set character=%q for %s\n", characterName, entry.Name())
		}
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Sentiment complete. Processed %d files, skipped %d.\n", processedCount, skippedCount)
	}

	return nil
}

func hasCharacterField(content string) bool {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}
		if inFrontmatter && strings.HasPrefix(strings.ToLower(trim), "character:") {
			return true
		}
	}
	return false
}

func insertCharacterFrontmatter(content, character string) string {
	lines := strings.Split(content, "\n")
	frontmatterEnd := -1

	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "---" {
			if frontmatterEnd == -1 {
				frontmatterEnd = 0
				continue
			}
			frontmatterEnd = i
			break
		}
	}

	if frontmatterEnd <= 0 {
		return fmt.Sprintf("---\ncharacter: \"%s\"\n---\n\n%s", character, content)
	}

	var sb strings.Builder
	for i, line := range lines {
		sb.WriteString(line)
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
		if i == frontmatterEnd-1 {
			sb.WriteString(fmt.Sprintf("\ncharacter: \"%s\"", character))
		}
	}

	return sb.String()
}

func determineCharacter(client *llm.Client, content string) (string, error) {
	systemPrompt := `You are analyzing a Discord RPG journal entry. Read the message content below and identify which in-character name the journal author uses.

This is the fictional character's name writing the journal, NOT the Discord username shown in the author metadata. The Discord author is the player; you want the character they are role-playing.

Return ONLY the character name. If you cannot determine a specific character name, return exactly "Unknown".`

	result, err := client.Chat(systemPrompt, content)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result), nil
}