package missed

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"discord-retriever/internal/discord"
)

const sample = `# New Character Adventure
<t:1788996600:F> - <t:1788996600:R>
### **GM:** *Waiting for a GM to claim this adventure*
**Players**: 4-6
**Level Requirement:** 1-3
**Participants**:
* Thorn Oakenshield <@!595828633449398282>
* Jim "Jaw-Breaker" Johansson <@!1215003603358453780>
* Laurelin  <@!1066514910336536606>
* Verin <@!1492756992630980780>`

func TestIsWaitingAdventure(t *testing.T) {
	if !isWaitingAdventure(sample) {
		t.Fatal("expected sample to match waiting adventure")
	}
	if isWaitingAdventure("# Not an adventure\nsome text") {
		t.Fatal("did not expect non-adventure to match")
	}
}

func TestExpectedDate(t *testing.T) {
	got, ok := expectedDate(sample)
	if !ok {
		t.Fatal("expected a date to be parsed")
	}
	want := time.Unix(1788996600, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestExpectedDateMissing(t *testing.T) {
	if _, ok := expectedDate("no timestamp here"); ok {
		t.Fatal("did not expect a date for content without a timestamp")
	}
}

func TestSessionName(t *testing.T) {
	if got := sessionName(sample); got != "New Character Adventure" {
		t.Fatalf("expected session name, got %q", got)
	}
	if got := sessionName("no heading"); got != "" {
		t.Fatalf("expected empty session name, got %q", got)
	}
}

func TestSignUpLink(t *testing.T) {
	msg := discord.Message{
		Components: []discord.Component{
			{
				Type: 1,
				Components: []discord.Component{
					{Type: 2, Label: "View Details & Sign Up", URL: "https://discord.com/channels/1/2/3"},
				},
			},
		},
	}
	if got := signUpLink(msg); got != "https://discord.com/channels/1/2/3" {
		t.Fatalf("expected sign up link, got %q", got)
	}
}

func TestSignUpLinkMissing(t *testing.T) {
	msg := discord.Message{
		Components: []discord.Component{
			{
				Type: 1,
				Components: []discord.Component{
					{Type: 2, Label: "Something Else", URL: "https://example.com"},
				},
			},
		},
	}
	if got := signUpLink(msg); got != "" {
		t.Fatalf("expected empty link, got %q", got)
	}
}

func TestCollectDeletedSession(t *testing.T) {
	content := `# [DELETED] twins seeking power
<t:1788048000:F> - <t:1788048000:R>
### **GM:** *Waiting for a GM to claim this adventure*
**Players**: 4-6
**Level Requirement:** 3-7
**Participants**:
* Remy LeBeau <@!595828633449398282>`
	msg := discord.Message{
		Author:     discord.User{ID: "595828633449398282"},
		Content:    content,
		Components: nil,
	}
	got := collect([]discord.Message{msg}, "595828633449398282", time.Unix(1788000000, 0).UTC())
	if len(got) != 1 {
		t.Fatalf("expected 1 deleted adventure, got %d", len(got))
	}
	if got[0].Session != "[DELETED] twins seeking power" {
		t.Fatalf("expected deleted session name, got %q", got[0].Session)
	}
	if got[0].Link != deletedMarker {
		t.Fatalf("expected link %q, got %q", deletedMarker, got[0].Link)
	}
	if !got[0].Date.Equal(time.Unix(1788048000, 0).UTC()) {
		t.Fatalf("expected date, got %v", got[0].Date)
	}
}

func TestCollectNonDeletedMissingButton(t *testing.T) {
	msg := discord.Message{
		Author:     discord.User{ID: "595828633449398282"},
		Content:    sample,
		Components: nil,
	}
	got := collect([]discord.Message{msg}, "595828633449398282", time.Unix(1788000000, 0).UTC())
	if len(got) != 0 {
		t.Fatalf("expected non-deleted session without a button to be skipped, got %d", len(got))
	}
}

func TestCollectDeletedSessionStillNeedsDateAndHeading(t *testing.T) {
	msg := discord.Message{
		Author:     discord.User{ID: "595828633449398282"},
		Content:    "# [DELETED] twins seeking power\nno timestamp, no marker",
		Components: nil,
	}
	got := collect([]discord.Message{msg}, "595828633449398282", time.Unix(1788000000, 0).UTC())
	if len(got) != 0 {
		t.Fatalf("expected deleted session missing required pieces to be skipped, got %d", len(got))
	}
}

func TestRenderText(t *testing.T) {
	adventures := []Adventure{
		{Session: "New Character Adventure", Date: time.Unix(1788996600, 0).UTC(), Link: "https://example.com/1"},
		{Session: "Another Session", Date: time.Unix(1789000000, 0).UTC(), Link: "https://example.com/2"},
	}

	var buf bytes.Buffer
	if err := render(&buf, adventures, "text"); err != nil {
		t.Fatalf("render text: %v", err)
	}

	want := "New Character Adventure 2026-09-09 https://example.com/1\n" +
		"Another Session 2026-09-10 https://example.com/2\n"
	if got := buf.String(); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRenderCSV(t *testing.T) {
	adventures := []Adventure{
		{Session: "New, Character Adventure", Date: time.Unix(1788996600, 0).UTC(), Link: "https://example.com/1"},
	}

	var buf bytes.Buffer
	if err := render(&buf, adventures, "csv"); err != nil {
		t.Fatalf("render csv: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 row, got %d lines: %q", len(lines), buf.String())
	}
	if lines[0] != "session,date,link" {
		t.Fatalf("expected header %q, got %q", "session,date,link", lines[0])
	}
	// The comma in the session name must be quoted.
	want := `"New, Character Adventure",2026-09-09,https://example.com/1`
	if lines[1] != want {
		t.Fatalf("expected row %q, got %q", want, lines[1])
	}
}

func TestRenderUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := render(&buf, nil, "xml"); err == nil {
		t.Fatal("expected an error for unknown format")
	}
}
