package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"discord-retriever/discord"
)

type Writer struct {
	outputDir string
	verbose   bool
	client    *discord.Client
}

func NewWriter(outputDir string, verbose bool, client *discord.Client) *Writer {
	return &Writer{
		outputDir: outputDir,
		verbose:   verbose,
		client:    client,
	}
}

func (w *Writer) WriteMessage(msg discord.Message, channelID string) error {
	filename := filepath.Join(w.outputDir, fmt.Sprintf("%s.md", msg.ID))

	content := w.renderMarkdown(msg, channelID)

	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (w *Writer) renderMarkdown(msg discord.Message, channelID string) string {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("# %s\n\n", msg.ID))

	// Metadata
	sb.WriteString(fmt.Sprintf("**Author**: %s (%s)\n", msg.Author.Username, msg.Author.ID))
	sb.WriteString(fmt.Sprintf("**Timestamp**: %s\n", msg.Timestamp))
	sb.WriteString(fmt.Sprintf("**Channel**: %s\n", channelID))
	sb.WriteString("\n---\n\n")

	// Message content
	if msg.Content != "" {
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	// Replies
	if msg.MessageReference != nil && msg.MessageReference.MessageID != "" {
		sb.WriteString("## Replies\n")
		sb.WriteString(fmt.Sprintf("**Replying to**: %s\n\n", msg.MessageReference.MessageID))
	}

	// Reactions
	if len(msg.Reactions) > 0 {
		sb.WriteString("## Reactions\n")
		for _, reaction := range msg.Reactions {
			sb.WriteString(fmt.Sprintf("- %s × %d\n", reaction.Emoji.Name, reaction.Count))
		}
		sb.WriteString("\n")
	}

	// Embeds
	if len(msg.Embeds) > 0 {
		sb.WriteString("## Embeds\n\n")
		for _, embed := range msg.Embeds {
			if embed.Title != "" {
				sb.WriteString(fmt.Sprintf("### %s\n", embed.Title))
			}
			if embed.URL != "" {
				sb.WriteString(fmt.Sprintf("**URL**: %s\n", embed.URL))
			}
			if embed.Description != "" {
				sb.WriteString(fmt.Sprintf("**Description**: %s\n", embed.Description))
			}
			if embed.Author != nil && embed.Author.Name != "" {
				sb.WriteString(fmt.Sprintf("**Author**: %s\n", embed.Author.Name))
			}
			if embed.Footer != nil && embed.Footer.Text != "" {
				sb.WriteString(fmt.Sprintf("**Footer**: %s\n", embed.Footer.Text))
			}
			sb.WriteString("\n")
		}
	}

	// Attachments
	if len(msg.Attachments) > 0 {
		sb.WriteString("## Attachments\n")
		for _, attachment := range msg.Attachments {
			localFilename := fmt.Sprintf("%s_%s", msg.ID, attachment.Filename)
			localPath := filepath.Join("attachments", localFilename)

			// Download the attachment
			downloaded := w.downloadAttachment(attachment, localFilename)

			if downloaded {
				sb.WriteString(fmt.Sprintf("- [%s](./%s)\n", attachment.Filename, localPath))
			} else {
				sb.WriteString(fmt.Sprintf("- [%s](./%s) (download failed)\n", attachment.Filename, localPath))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (w *Writer) downloadAttachment(attachment discord.Attachment, localFilename string) bool {
	attachmentsDir := filepath.Join(w.outputDir, "attachments")
	localPath := filepath.Join(attachmentsDir, localFilename)

	// Check if file already exists
	if _, err := os.Stat(localPath); err == nil {
		if w.verbose {
			fmt.Fprintf(os.Stderr, "  Attachment already exists: %s\n", attachment.Filename)
		}
		return true
	}

	// Download using the client with retry handling
	data, err := w.client.DownloadAttachment(attachment.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to download attachment %s: %v\n", attachment.Filename, err)
		return false
	}

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save attachment %s: %v\n", attachment.Filename, err)
		return false
	}

	if w.verbose {
		fmt.Fprintf(os.Stderr, "  Downloaded attachment: %s\n", attachment.Filename)
	}

	return true
}
