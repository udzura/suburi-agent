package main

import (
	"context"
	"fmt"
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
	if err := consumeResponse(session, resp); err != nil {
		panic(err)
	}
}

func consumeResponse(session *genai.ChatSession, resp *genai.GenerateContentResponse) error {
	for _, part := range resp.Candidates[0].Content.Parts {
		switch part := part.(type) {
		case genai.Text:
			fmt.Printf("RESP: %s\n", string(part))
		case genai.FunctionCall:
			fmt.Printf("CALL: %s\n", part.Name)
			res, err := verifyAndRunFunctionCall(session, part)
			if err != nil {
				return err
			}
			return consumeResponse(session, res)
		default:
			fmt.Printf("Unexpected content type: %T, %v\n", part, part)
		}
	}

	return nil
}

func verifyAndRunFunctionCall(session *genai.ChatSession, call genai.FunctionCall) (*genai.GenerateContentResponse, error) {
	switch call.Name {
	case "time_now":
		// No arg size validation
		now, err := callTimeNow()
		if err != nil {
			return nil, err
		}
		reply := genai.FunctionResponse{
			Name: "time_now",
			Response: map[string]any{
				"current_time": now,
			},
		}
		return session.SendMessage(context.Background(), reply)
	default:
		return nil, fmt.Errorf("unknown function call: %s", call.Name)
	}
}

func callTimeNow() (string, error) {
	currentTime := time.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf("Current time in UTC: %s", currentTime), nil
}
