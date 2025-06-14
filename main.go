package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

var calClient *calendar.Service

func main() {
	ctx := context.Background()
	// Create a new client with the default configuration
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GENAI_API_KEY")))
	if err != nil {
		panic(err)
	}

	client2, err := calendar.NewService(ctx, option.WithAPIKey(os.Getenv("GOOGLE_CALENDAR_API_KEY")))
	if err != nil {
		panic(fmt.Errorf("failed to create calendar client: %w", err))
	}
	calClient = client2

	// model := client.GenerativeModel("gemma-3-12b-it")
	model := client.GenerativeModel("gemini-2.0-flash")
	model.Tools = []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "time_now",
					Description: "Get the current time in UTC",
				},
				{
					Name:        "calendar_event_list",
					Description: "List upcoming calendar events from now",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"count": {
								Type:        genai.TypeInteger,
								Description: "Number of upcoming events to list",
							},
						},
					},
				},
				{
					Name:        "calendar_event_register",
					Description: "Register a new calendar event",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"start": {
								Type:        genai.TypeString,
								Description: "Start time of the event in RFC3339 format",
							},
							"end": {
								Type:        genai.TypeString,
								Description: "End time of the event in RFC3339 format",
							},
							"summary": {
								Type:        genai.TypeString,
								Description: "Title of the event",
							},
							"description": {
								Type:        genai.TypeString,
								Description: "Simple description of the event",
							},
						},
					},
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
			Name: call.Name,
			Response: map[string]any{
				"current_time": now,
			},
		}
		return session.SendMessage(context.Background(), reply)
	case "calendar_event_list":
		// Validate the argument size
		if len(call.Args) == 0 {
			return nil, fmt.Errorf("missing required argument 'count' for function call: %s", call.Name)
		}
		if _, ok := call.Args["count"]; !ok {
			return nil, fmt.Errorf("invalid argument type for 'count': expected int64, got %v", call.Args)
		}
		if _, ok := call.Args["count"].(int64); !ok {
			return nil, fmt.Errorf("invalid argument type for 'count': expected int64, got %v", call.Args)
		}

		events, err := callEventList(call.Args["count"].(int64))
		var reply genai.FunctionResponse
		if err != nil {
			reply = genai.FunctionResponse{
				Name: call.Name,
				Response: map[string]any{
					"error": fmt.Sprintf("Failed to list events: %v", err),
				},
			}
		} else {
			reply = genai.FunctionResponse{
				Name: call.Name,
				Response: map[string]any{
					"events": events,
				},
			}
		}

		return session.SendMessage(context.TODO(), reply)
	case "calendar_event_register":
		// Validate the argument size
		if len(call.Args) < 4 {
			return nil, fmt.Errorf("missing required arguments for function call: %s", call.Name)
		}
		// FIXME: Validate the argument types...

		createdEventLink, err := callEventRegister(
			call.Args["start"].(string),
			call.Args["end"].(string),
			call.Args["summary"].(string),
			call.Args["description"].(string),
		)
		var reply genai.FunctionResponse
		if err != nil {
			reply = genai.FunctionResponse{
				Name: call.Name,
				Response: map[string]any{
					"error": fmt.Sprintf("Failed to register event: %v", err),
				},
			}
		} else {
			reply = genai.FunctionResponse{
				Name: call.Name,
				Response: map[string]any{
					"summary":     call.Args["summary"].(string),
					"description": call.Args["description"].(string),
					"start":       call.Args["start"].(string),
					"end":         call.Args["end"].(string),
					"event_link":  createdEventLink,
				},
			}
		}
		return session.SendMessage(context.TODO(), reply)
	default:
		return nil, fmt.Errorf("unknown function call: %s", call.Name)
	}
}

func callTimeNow() (string, error) {
	currentTime := time.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf("Current time in UTC: %s", currentTime), nil
}

func callEventList(n int64) ([]map[string]any, error) {
	events, err := calClient.Events.
		List("primary").
		TimeMin(time.Now().Format(time.RFC3339)).
		MaxResults(n).
		SingleEvents(true).
		OrderBy("startTime").
		Do()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve events: %w", err)
	}
	eventSlice := make([]map[string]any, 0)

	for _, item := range events.Items {
		event := map[string]any{
			"id":          item.Id,
			"summary":     item.Summary,
			"description": item.Description,
			"start":       item.Start.DateTime,
			"end":         item.End.DateTime,
		}
		eventSlice = append(eventSlice, event)
	}
	return eventSlice, nil
}

func callEventRegister(start string, end string, summary string, description string) (string, error) {
	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: start,
			TimeZone: "Asia/Tokyo",
		},
		End: &calendar.EventDateTime{
			DateTime: end,
			TimeZone: "Asia/Tokyo",
		},
	}

	createdEvent, err := calClient.Events.Insert("primary", event).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create event: %w", err)
	}
	return createdEvent.HtmlLink, nil
}
