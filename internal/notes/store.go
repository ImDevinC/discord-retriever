package notes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store manages the Obsidian vault on disk.
type Store struct {
	notesDir string
	verbose  bool
}

// NewStore creates a vault store rooted at notesDir.
func NewStore(notesDir string, verbose bool) *Store {
	return &Store{
		notesDir: notesDir,
		verbose:  verbose,
	}
}

func (s *Store) categoryDir(category string) string {
	return filepath.Join(s.notesDir, strings.ToLower(category))
}

func (s *Store) ensureCategoryDir(category string) error {
	return os.MkdirAll(s.categoryDir(category), 0755)
}

// --- fuzzy matching helpers ---

func normalizeString(str string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(str) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func wordSet(str string) map[string]bool {
	words := strings.Fields(normalizeString(str))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

func isSubset(a, b map[string]bool) bool {
	if len(a) > len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func overlapScore(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// FindMatch searches a single category directory for a note whose title
// fuzzily matches the given title.  Matching is word-set based:
// one title's word set must be a subset of the other's.
func (s *Store) FindMatch(title, category string) (string, bool) {
	dir := s.categoryDir(category)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	targetWords := wordSet(title)
	if len(targetWords) == 0 {
		return "", false
	}

	var bestMatch string
	var bestScore float64

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		candidate := strings.TrimSuffix(entry.Name(), ".md")
		candidateWords := wordSet(candidate)
		if len(candidateWords) == 0 {
			continue
		}

		matched := isSubset(targetWords, candidateWords) || isSubset(candidateWords, targetWords)
		score := overlapScore(targetWords, candidateWords)

		if matched && score > bestScore {
			bestScore = score
			bestMatch = filepath.Join(dir, entry.Name())
		}
	}

	return bestMatch, bestMatch != ""
}

// HasSourceMessage quickly scans the YAML frontmatter of a note for a
// message-ID string.  Used to avoid duplicating updates on re-runs.
func (s *Store) HasSourceMessage(path, msgID string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	lines := strings.Split(string(data), "\n")
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
		if inFrontmatter && strings.Contains(line, msgID) {
			return true
		}
	}
	return false
}

// SetFrontmatterField sets a frontmatter field if not already present (first-set-wins).
// If the field already exists, this is a no-op.
func (s *Store) SetFrontmatterField(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inFrontmatter := false
	frontmatterEnd := -1

	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			frontmatterEnd = i
			break
		}
		if inFrontmatter && strings.HasPrefix(strings.ToLower(trim), key+":") {
			return nil
		}
	}

	if frontmatterEnd == -1 {
		return nil
	}

	var sb strings.Builder
	for i, line := range lines {
		sb.WriteString(line)
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
		if i == frontmatterEnd-1 {
			sb.WriteString(fmt.Sprintf("\n%s: \"%s\"", key, value))
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// WriteNew creates a brand-new note with frontmatter.
func (s *Store) WriteNew(title, category, content, firstSeen, sourceMsg string, tags []string, discordUsername string) error {
	if err := s.ensureCategoryDir(category); err != nil {
		return err
	}

	filename := sanitizeFilename(title) + ".md"
	path := filepath.Join(s.categoryDir(category), filename)

	allTags := []string{strings.ToLower(category)}
	for _, t := range tags {
		lt := strings.ToLower(strings.TrimSpace(t))
		if lt != "" && lt != strings.ToLower(category) {
			allTags = append(allTags, lt)
		}
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("title: \"%s\"\n", title))
	sb.WriteString(fmt.Sprintf("category: %s\n", category))
	sb.WriteString("tags: [")
	for i, t := range allTags {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(t)
	}
	sb.WriteString("]\n")
	sb.WriteString("source_messages:\n")
	sb.WriteString(fmt.Sprintf("  - \"%s\"\n", sourceMsg))
	sb.WriteString(fmt.Sprintf("first_seen: \"%s\"\n", firstSeen))
	if discordUsername != "" {
		sb.WriteString(fmt.Sprintf("discord_username: \"%s\"\n", discordUsername))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// AppendUpdate adds an "UPDATED INFORMATION" section to an existing note.
func (s *Store) AppendUpdate(path, sourceFile, newContent string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read existing note: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(string(existing))
	if !strings.HasSuffix(string(existing), "\n") {
		sb.WriteString("\n")
	}
	if !strings.HasSuffix(string(existing), "\n\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("## UPDATED INFORMATION\n")
	sb.WriteString(fmt.Sprintf("*(from %s)*\n\n", sourceFile))
	sb.WriteString(newContent)
	if !strings.HasSuffix(newContent, "\n") {
		sb.WriteString("\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// ReadFile returns the contents of a note.
func (s *Store) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile overwrites a note in its entirety.
func (s *Store) WriteFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// AllFiles returns every .md file path across all three category directories.
func (s *Store) AllFiles() ([]string, error) {
	var files []string
	for _, cat := range []string{"people", "places", "events"} {
		dir := filepath.Join(s.notesDir, cat)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				files = append(files, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return files, nil
}

// AllTitles returns a deduplicated slice of every note title (filename without
// extension) across all category directories.
func (s *Store) AllTitles() ([]string, error) {
	seen := make(map[string]bool)
	var titles []string
	for _, cat := range []string{"people", "places", "events"} {
		dir := filepath.Join(s.notesDir, cat)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				title := strings.TrimSuffix(entry.Name(), ".md")
				if !seen[title] {
					seen[title] = true
					titles = append(titles, title)
				}
			}
		}
	}
	return titles, nil
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	return s
}
