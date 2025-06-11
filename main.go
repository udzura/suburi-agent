package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()
	// Create a new client with the default configuration
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GENAI_API_KEY")))
	if err != nil {
		panic(err)
	}

	// model := client.GenerativeModel("gemma-3-12b-it")
	model := client.GenerativeModel("gemini-2.0-flash")
	model.Tools = []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "time_now",
					Description: "Get the current time in UTC",
				},
			},
			// CodeExecution: new(genai.CodeExecution),
		},
	}
	session := model.StartChat()
	var prompt string
	if _, err := fmt.Scanln(&prompt); err != nil {
		panic(err)
	}

	resp, err := session.SendMessage(ctx, genai.Text(prompt))
	if err != nil {
		panic(err)
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		switch part := part.(type) {
		case genai.FunctionCall:
			fmt.Printf("CALL: %s\n", part.Name)
			if part.Name == "time_now" {
				now := time.Now().UTC().Format(time.RFC3339)
				response := genai.FunctionResponse{
					Name: "find_theaters",
					Response: map[string]any{
						"current_time": now,
					},
				}
				resp2, err := session.SendMessage(ctx, response)
				if err != nil {
					panic(err)
				}
				for _, part := range resp2.Candidates[0].Content.Parts {
					switch part := part.(type) {
					case genai.Text:
						fmt.Printf("RESP: %s\n", string(part))
					default:
						fmt.Printf("Unexpected content type: %T\n", part)
					}
				}
			} else {
				log.Fatalf("Unexpected function call: %s\n", part.Name)
			}
		case genai.Text:
			fmt.Printf("RESP: %s\n", string(part))
		default:
			fmt.Printf("Unexpected content type: %T\n", part)
		}
	}
}
