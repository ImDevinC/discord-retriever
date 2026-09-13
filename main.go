package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"discord-retriever/internal/discord"
	"discord-retriever/internal/llm"
	"discord-retriever/internal/missed"
	"discord-retriever/internal/review"
	"discord-retriever/internal/sentiment"
	"discord-retriever/internal/writer"
)

type Config struct {
	Token   string
	Channel string
	Output  string
	Verbose bool
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "get-messages":
		cfg := parseGetMessagesFlags()
		if err := run(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "review":
		cfg := parseReviewFlags()
		if err := runReview(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "missed":
		cfg := parseMissedFlags()
		if err := missed.Run(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "sentiment":
		cfg := parseSentimentFlags()
		if err := runSentiment(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("discord-retriever - fetch messages from a Discord channel")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  get-messages    Fetch and save messages from a Discord channel")
	fmt.Println("  review          Extract structured notes from downloaded messages via LLM")
	fmt.Println("  missed          List unclaimed adventures after a start date from a channel")
	fmt.Println("  sentiment       Identify character names for journal entries via LLM")
	fmt.Println()
	fmt.Println("Run 'discord-retriever <command> --help' for command-specific flags.")
}

func parseGetMessagesFlags() Config {
	var cfg Config

	cmd := flag.NewFlagSet("get-messages", flag.ExitOnError)
	cmd.StringVar(&cfg.Token, "token", "", "User token (or DISCORD_TOKEN env var)")
	cmd.StringVar(&cfg.Channel, "channel", "", "Channel ID (or DISCORD_CHANNEL_ID env var)")
	cmd.StringVar(&cfg.Output, "output", "./output", "Output directory")
	cmd.BoolVar(&cfg.Verbose, "verbose", false, "Print progress to stderr")

	cmd.Parse(os.Args[2:])

	// Environment variable fallback
	if cfg.Token == "" {
		cfg.Token = os.Getenv("DISCORD_TOKEN")
	}
	if cfg.Channel == "" {
		cfg.Channel = os.Getenv("DISCORD_CHANNEL_ID")
	}

	// Validate required parameters
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Error: token is required (use --token or DISCORD_TOKEN env var)")
		cmd.Usage()
		os.Exit(1)
	}

	if cfg.Channel == "" {
		fmt.Fprintln(os.Stderr, "Error: channel is required (use --channel or DISCORD_CHANNEL_ID env var)")
		cmd.Usage()
		os.Exit(1)
	}

	return cfg
}

func parseMissedFlags() missed.Config {
	var cfg missed.Config

	cmd := flag.NewFlagSet("missed", flag.ExitOnError)
	cmd.StringVar(&cfg.Token, "token", "", "User token (or DISCORD_TOKEN env var)")
	cmd.StringVar(&cfg.Channel, "channel", "", "Channel ID")
	cmd.StringVar(&cfg.StartDate, "start-date", "", "Start date (YYYY-MM-DD)")
	cmd.StringVar(&cfg.UserID, "user-id", "", "User ID to scan messages for")
	cmd.BoolVar(&cfg.Verbose, "verbose", false, "Print progress to stderr")
	cmd.StringVar(&cfg.Format, "format", "text", "Output format (text or csv)")

	cmd.Parse(os.Args[2:])

	if cfg.Token == "" {
		cfg.Token = os.Getenv("DISCORD_TOKEN")
	}

	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Error: token is required (use --token or DISCORD_TOKEN env var)")
		cmd.Usage()
		os.Exit(1)
	}
	if cfg.Channel == "" {
		fmt.Fprintln(os.Stderr, "Error: channel is required (use --channel)")
		cmd.Usage()
		os.Exit(1)
	}
	if cfg.StartDate == "" {
		fmt.Fprintln(os.Stderr, "Error: start-date is required (use --start-date YYYY-MM-DD)")
		cmd.Usage()
		os.Exit(1)
	}
	if cfg.UserID == "" {
		fmt.Fprintln(os.Stderr, "Error: user-id is required (use --user-id)")
		cmd.Usage()
		os.Exit(1)
	}

	return cfg
}

func parseReviewFlags() review.Config {
	var cfg review.Config

	cmd := flag.NewFlagSet("review", flag.ExitOnError)
	apiKey := cmd.String("api-key", "", "LLM API key (or OPENAI_API_KEY env var)")
	baseURL := cmd.String("base-url", "", "LLM API base URL (or OPENAI_BASE_URL env var)")
	model := cmd.String("model", "", "LLM model name (or LLM_MODEL env var)")
	cmd.StringVar(&cfg.InputDir, "input", "./output", "Directory with raw message markdown files")
	cmd.StringVar(&cfg.NotesDir, "notes", "./notes", "Obsidian vault / notes output directory")
	cmd.BoolVar(&cfg.Verbose, "verbose", false, "Print progress to stderr")
	filesOpt := cmd.String("files", "", "Comma-separated filenames to process (omit to process all .md files)")


	cmd.Parse(os.Args[2:])

	if *filesOpt != "" {
		cfg.Files = strings.Split(*filesOpt, ",")
	}

	cmd.Parse(os.Args[2:])

	// Environment variable fallbacks
	if *apiKey == "" {
		*apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if *baseURL == "" {
		*baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if *model == "" {
		*model = os.Getenv("LLM_MODEL")
	}

	// Validate required LLM parameters
	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: api-key is required (use --api-key or OPENAI_API_KEY env var)")
		cmd.Usage()
		os.Exit(1)
	}
	if *baseURL == "" {
		fmt.Fprintln(os.Stderr, "Error: base-url is required (use --base-url or OPENAI_BASE_URL env var)")
		cmd.Usage()
		os.Exit(1)
	}
	if *model == "" {
		fmt.Fprintln(os.Stderr, "Error: model is required (use --model or LLM_MODEL env var)")
		cmd.Usage()
		os.Exit(1)
	}

	cfg.LLM = llm.NewClient(*baseURL, *apiKey, *model)
	return cfg
}

func runReview(cfg review.Config) error {
	return review.Run(cfg)
}

func parseSentimentFlags() sentiment.Config {
	var cfg sentiment.Config

	cmd := flag.NewFlagSet("sentiment", flag.ExitOnError)
	apiKey := cmd.String("api-key", "", "LLM API key (or OPENAI_API_KEY env var)")
	baseURL := cmd.String("base-url", "", "LLM API base URL (or OPENAI_BASE_URL env var)")
	model := cmd.String("model", "", "LLM model name (or LLM_MODEL env var)")
	cmd.StringVar(&cfg.InputDir, "input", "./output", "Directory with raw message markdown files")
	cmd.BoolVar(&cfg.Verbose, "verbose", false, "Print progress to stderr")
	filesOpt := cmd.String("files", "", "Comma-separated filenames to process (omit to process all .md files)")

	cmd.Parse(os.Args[2:])

	if *filesOpt != "" {
		cfg.Files = strings.Split(*filesOpt, ",")
	}

	// Environment variable fallbacks
	if *apiKey == "" {
		*apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if *baseURL == "" {
		*baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if *model == "" {
		*model = os.Getenv("LLM_MODEL")
	}

	// Validate required LLM parameters
	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: api-key is required (use --api-key or OPENAI_API_KEY env var)")
		cmd.Usage()
		os.Exit(1)
	}
	if *baseURL == "" {
		fmt.Fprintln(os.Stderr, "Error: base-url is required (use --base-url or OPENAI_BASE_URL env var)")
		cmd.Usage()
		os.Exit(1)
	}
	if *model == "" {
		fmt.Fprintln(os.Stderr, "Error: model is required (use --model or LLM_MODEL env var)")
		cmd.Usage()
		os.Exit(1)
	}

	cfg.LLM = llm.NewClient(*baseURL, *apiKey, *model)
	return cfg
}

func runSentiment(cfg sentiment.Config) error {
	return sentiment.Run(cfg)
}

func run(cfg Config) error {
	// Create output directories
	if err := os.MkdirAll(cfg.Output, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	attachmentsDir := cfg.Output + "/attachments"
	if err := os.MkdirAll(attachmentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create attachments directory: %w", err)
	}

	// Initialize components
	client := discord.NewClient(cfg.Token, cfg.Verbose)
	writer := writer.NewWriter(cfg.Output, cfg.Verbose, client)

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Fetching messages from channel %s...\n", cfg.Channel)
	}

	// Fetch all messages
	start := time.Now()
	messages, err := client.FetchAllMessages(cfg.Channel)
	if err != nil {
		return fmt.Errorf("failed to fetch messages: %w", err)
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Fetched %d messages in %v\n", len(messages), time.Since(start))
	}

	// Check for existing files to support resume
	existingFiles := make(map[string]bool)
	entries, err := os.ReadDir(cfg.Output)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && len(entry.Name()) > 3 && entry.Name()[len(entry.Name())-3:] == ".md" {
				existingFiles[entry.Name()] = true
			}
		}
	}

	// Write messages (reverse to chronological order)
	skippedCount := 0
	writtenCount := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		filename := fmt.Sprintf("%s.md", msg.ID)

		// Check if already exists (resume support)
		if existingFiles[filename] {
			skippedCount++
			continue
		}

		if err := writer.WriteMessage(msg, cfg.Channel); err != nil {
			return fmt.Errorf("failed to write message %s: %w", msg.ID, err)
		}
		writtenCount++
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Wrote %d new messages, skipped %d existing\n", writtenCount, skippedCount)
	}

	return nil
}
