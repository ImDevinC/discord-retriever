package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- internal matching helpers ---

func TestNormalizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"John Smith", "john smith"},
		{"The Hungry Dragon Inn!", "the hungry dragon inn"},
		{"Soreaya (Squirrel)", "soreaya squirrel"},
		{"", ""},
		{"123-ABC", "123abc"},
	}
	for _, tt := range tests {
		got := normalizeString(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestWordSet(t *testing.T) {
	set := wordSet("John Smith")
	if len(set) != 2 {
		t.Fatalf("expected 2 words, got %d", len(set))
	}
	if !set["john"] || !set["smith"] {
		t.Error("expected 'john' and 'smith' in set")
	}
}

func TestIsSubset(t *testing.T) {
	a := map[string]bool{"john": true}
	b := map[string]bool{"john": true, "smith": true}
	if !isSubset(a, b) {
		t.Error("expected {john} to be subset of {john, smith}")
	}
	if isSubset(b, a) {
		t.Error("expected {john, smith} NOT to be subset of {john}")
	}
}

func TestOverlapScore(t *testing.T) {
	a := map[string]bool{"john": true}
	b := map[string]bool{"john": true, "smith": true}
	score := overlapScore(a, b)
	if score != 0.5 {
		t.Errorf("expected 0.5, got %f", score)
	}
}

// --- FindMatch (category-scoped) ---

func TestFindMatch(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	// Seed one category directory with a couple of files
	peopleDir := filepath.Join(tmp, "people")
	os.MkdirAll(peopleDir, 0755)
	os.WriteFile(filepath.Join(peopleDir, "John Smith.md"), []byte("# John Smith\n"), 0644)
	os.WriteFile(filepath.Join(peopleDir, "Jane Doe.md"), []byte("# Jane Doe\n"), 0644)

	placesDir := filepath.Join(tmp, "places")
	os.MkdirAll(placesDir, 0755)
	os.WriteFile(filepath.Join(placesDir, "The Hungry Dragon Inn.md"), []byte("# The Hungry Dragon Inn\n"), 0644)

	cases := []struct {
		title       string
		category    string
		wantFound   bool
		wantSuffix  string
	}{
		{"John Smith", "People", true, "John Smith.md"},
		{"John", "People", true, "John Smith.md"},       // fuzzy: subset match
		{"Smith", "People", true, "John Smith.md"},      // fuzzy: subset match
		{"Jane", "People", true, "Jane Doe.md"},         // fuzzy: subset match
		{"John", "Places", false, ""},                   // different category
		{"Dragon Inn", "Places", true, "The Hungry Dragon Inn.md"}, // fuzzy
		{"Nonexistent", "People", false, ""},
	}

	for _, c := range cases {
		path, found := store.FindMatch(c.title, c.category)
		if found != c.wantFound {
			t.Errorf("FindMatch(%q, %q) found=%v, want %v", c.title, c.category, found, c.wantFound)
			continue
		}
		if found && !strings.HasSuffix(path, c.wantSuffix) {
			t.Errorf("FindMatch(%q, %q) path=%q, want suffix %q", c.title, c.category, path, c.wantSuffix)
		}
	}
}

// --- HasSourceMessage ---

func TestHasSourceMessage(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	content := `---
title: "John Smith"
category: People
source_messages:
  - "1281429034735108178"
first_seen: "2024-09-06"
---

# John Smith

Some content.
`
	path := filepath.Join(tmp, "people", "John Smith.md")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)

	if !store.HasSourceMessage(path, "1281429034735108178") {
		t.Error("expected to find source message in frontmatter")
	}
	if store.HasSourceMessage(path, "9999999999999999999") {
		t.Error("expected NOT to find unrelated source message")
	}
}

// --- WriteNew / AppendUpdate round-trip ---

func TestWriteNewAndAppendUpdate(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	// Write a new note
	err := store.WriteNew("Charlie", "People", "Charlie is a raccoon.", "2024-09-06", "1281429034735108178", []string{"raccoon", "npc"}, "")
	if err != nil {
		t.Fatalf("WriteNew failed: %v", err)
	}

	path := filepath.Join(tmp, "people", "Charlie.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	written := string(data)
	if !strings.Contains(written, `title: "Charlie"`) {
		t.Error("missing title frontmatter")
	}
	if !strings.Contains(written, `first_seen: "2024-09-06"`) {
		t.Error("missing first_seen frontmatter")
	}
	if !strings.Contains(written, "Charlie is a raccoon.") {
		t.Error("missing body content")
	}

	// Append an update
	err = store.AppendUpdate(path, "1404879949135085578.md", "Charlie was seen at the inn.")
	if err != nil {
		t.Fatalf("AppendUpdate failed: %v", err)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	updated := string(data)
	if !strings.Contains(updated, "## UPDATED INFORMATION") {
		t.Error("missing UPDATED INFORMATION section")
	}
	if !strings.Contains(updated, "Charlie was seen at the inn.") {
		t.Error("missing appended content")
	}
}

// --- AllFiles / AllTitles ---

func TestAllFilesAndTitles(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	os.MkdirAll(filepath.Join(tmp, "people"), 0755)
	os.MkdirAll(filepath.Join(tmp, "places"), 0755)
	os.MkdirAll(filepath.Join(tmp, "events"), 0755)
	os.WriteFile(filepath.Join(tmp, "people", "A.md"), []byte("# A\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "places", "B.md"), []byte("# B\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "events", "C.md"), []byte("# C\n"), 0644)

	files, err := store.AllFiles()
	if err != nil {
		t.Fatalf("AllFiles failed: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}

	titles, err := store.AllTitles()
	if err != nil {
		t.Fatalf("AllTitles failed: %v", err)
	}
	if len(titles) != 3 {
		t.Errorf("expected 3 titles, got %d", len(titles))
	}
}

func TestWriteNewWithDiscordUsername(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	err := store.WriteNew("John", "People", "John is a developer.", "2024-09-06", "msg123", []string{"developer"}, "johndoe")
	if err != nil {
		t.Fatalf("WriteNew failed: %v", err)
	}

	path := filepath.Join(tmp, "people", "John.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	written := string(data)
	if !strings.Contains(written, `discord_username: "johndoe"`) {
		t.Error("missing discord_username frontmatter")
	}
}

func TestSetFrontmatterFieldFirstSetWins(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	path := filepath.Join(tmp, "people", "Test.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	content := "---\ntitle: \"Test\"\ncategory: People\ntags: [people]\nsource_messages: [\"msg1\"]\nfirst_seen: \"2024-09-06\"\ndiscord_username: \"original\"\n---\n\n# Test\ncontent here\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err := store.SetFrontmatterField(path, "discord_username", "newuser")
	if err != nil {
		t.Fatalf("SetFrontmatterField failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	written := string(data)
	if !strings.Contains(written, `discord_username: "original"`) {
		t.Error("first-set-wins: original value should remain")
	}
	if strings.Contains(written, `discord_username: "newuser"`) {
		t.Error("first-set-wins: new value should not be added")
	}
}

func TestSetFrontmatterFieldInsertsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	path := filepath.Join(tmp, "people", "Test.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	content := "---\ntitle: \"Test\"\ncategory: People\ntags: [people]\nsource_messages: [\"msg1\"]\nfirst_seen: \"2024-09-06\"\n---\n\n# Test\ncontent here\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err := store.SetFrontmatterField(path, "discord_username", "newuser")
	if err != nil {
		t.Fatalf("SetFrontmatterField failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	written := string(data)
	if !strings.Contains(written, `discord_username: "newuser"`) {
		t.Error("should have added discord_username")
	}
}

func TestSetFrontmatterFieldNoFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp, false)

	path := filepath.Join(tmp, "people", "Test.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	content := "# Test\ncontent here\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err := store.SetFrontmatterField(path, "discord_username", "newuser")
	if err != nil {
		t.Fatalf("SetFrontmatterField failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	written := string(data)
	if strings.Contains(written, "discord_username") {
		t.Error("should not add frontmatter when none exists")
	}
}


