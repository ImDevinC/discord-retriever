package missed

import (
	"encoding/csv"
	"fmt"
	"io"
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
	deletedMarker = "[DELETED]"
)

var epochRe = regexp.MustCompile(`<t:(\d+):F>`)

type Config struct {
	Token     string
	Channel   string
	StartDate string
	UserID    string
	Verbose   bool
	Format    string
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

	adventures := collect(messages, cfg.UserID, startDate)
	if err := render(os.Stdout, adventures, cfg.Format); err != nil {
		return err
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Matched %d missed adventures\n", len(adventures))
	}

	return nil
}

type Adventure struct {
	Session string
	Date    time.Time
	Link    string
}

func collect(messages []discord.Message, userID string, startDate time.Time) []Adventure {
	var adventures []Adventure
	for _, msg := range messages {
		if msg.Author.ID != userID {
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
		if strings.HasPrefix(session, deletedMarker) {
			link = deletedMarker
		} else if link == "" {
			continue
		}

		adventures = append(adventures, Adventure{Session: session, Date: date, Link: link})
	}
	return adventures
}

func render(w io.Writer, adventures []Adventure, format string) error {
	switch format {
	case "text":
		for _, a := range adventures {
			fmt.Fprintf(w, "%s %s %s\n", a.Session, a.Date.Format("2006-01-02"), a.Link)
		}
	case "csv":
		cw := csv.NewWriter(w)
		if err := cw.Write([]string{"session", "date", "link"}); err != nil {
			return err
		}
		for _, a := range adventures {
			if err := cw.Write([]string{a.Session, a.Date.Format("2006-01-02"), a.Link}); err != nil {
				return err
			}
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown format %q (expected text or csv)", format)
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
