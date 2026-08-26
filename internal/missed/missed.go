package missed

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"discord-retriever/internal/discord"
)

const (
	waitingMarker = "### **GM:** *Waiting for a GM to claim this adventure*"
	signUpLabel   = "View Details & Sign Up"
)

var epochRe = regexp.MustCompile(`<t:(\d+):F>`)

type Config struct {
	Token     string
	Channel   string
	StartDate string
	UserID    string
	Verbose   bool
}

func Run(cfg Config) error {
	startDate, err := time.Parse("2006-01-02", cfg.StartDate)
	if err != nil {
		return fmt.Errorf("invalid start date %q (expected YYYY-MM-DD): %w", cfg.StartDate, err)
	}

	client := discord.NewClient(cfg.Token, cfg.Verbose)

	messages, err := client.FetchAllMessages(cfg.Channel)
	if err != nil {
		return fmt.Errorf("failed to fetch messages: %w", err)
	}

	count := 0
	for _, msg := range messages {
		if msg.Author.ID != cfg.UserID {
			continue
		}
		if !isWaitingAdventure(msg.Content) {
			continue
		}

		date, ok := expectedDate(msg.Content)
		if !ok {
			continue
		}
		if !date.After(startDate) || date.After(time.Now()) {
			continue
		}

		session := sessionName(msg.Content)
		if session == "" {
			continue
		}

		link := signUpLink(msg)
		if link == "" {
			continue
		}

		fmt.Printf("%s %s %s\n", session, date.Format("2006-01-02"), link)
		count++
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Matched %d missed adventures\n", count)
	}

	return nil
}

func isWaitingAdventure(content string) bool {
	return strings.Contains(content, waitingMarker)
}

func expectedDate(content string) (time.Time, bool) {
	match := epochRe.FindStringSubmatch(content)
	if match == nil {
		return time.Time{}, false
	}
	epoch, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(epoch, 0).UTC(), true
}

func sessionName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}

func signUpLink(msg discord.Message) string {
	for _, row := range msg.Components {
		for _, comp := range row.Components {
			if strings.Contains(comp.Label, signUpLabel) && comp.URL != "" {
				return comp.URL
			}
		}
	}
	return ""
}
