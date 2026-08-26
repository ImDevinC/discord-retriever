package missed

import (
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
