package server

import (
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
)

// This file is the one exit of the server (CLAUDE.md rule 2). Every CallToolResult and every
// TextContent is built here and nowhere else; exit_test.go fails when another file builds one.
// An answer goes through privacy.Out (scrubbed, 2-space JSON), an error through privacy.ScrubText.
// There is no structuredContent. The text is Danish, without the SDK's "Error executing tool" prefix.

// answer is a successful tool result: one text content with the scrubbed JSON of value.
func answer(value any) *mcp.CallToolResult {
	text, err := privacy.Out(value)
	if err != nil {
		slog.Error("answer could not be built", "type", fmt.Sprintf("%T", value))
		return failure("Svaret kunne ikke laves. Se serverens log.")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// failure is a tool error: isError with the Danish text only. The text is scrubbed, because a
// store's own message can echo personal data.
func failure(text string) *mcp.CallToolResult {
	clean, _ := privacy.ScrubText(text).(string)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: clean}},
		IsError: true,
	}
}
