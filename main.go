package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"discord-retriever/discord"
	"discord-retriever/writer"
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
