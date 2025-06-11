package main

import (
	"context"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create a new MCP server
	srv := server.NewMCPServer(
		"Telling current time",
		"1.0.0",
	)

	timeNow := mcp.NewTool(
		"time_now",
		mcp.WithDescription("Get the current time in UTC"),
	)
	srv.AddTool(timeNow, func(_ctx context.Context, _request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		currentTime := time.Now().UTC().Format(time.RFC3339)
		return mcp.NewToolResultText(currentTime), nil
	})

	httpd := server.NewStreamableHTTPServer(srv)
	log.Println("Starting MCP server on port 8080")
	if err := httpd.Start(":8080"); err != nil {
		panic(err)
	}
}
