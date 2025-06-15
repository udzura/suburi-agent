package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chzyer/readline"
	"github.com/google/generative-ai-go/genai"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

var calClient *calendar.Service

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	ctx, cancel := context.WithCancel(context.Background())
	pipe := make(chan string)
	go acceptTokenViaLocalHTTP(ctx, pipe)
	defer cancel()

	config.RedirectURL = "http://localhost:28080/"

	// AccessTypeOffline を指定することでリフレッシュトークンを取得する
	authURL := config.AuthCodeURL(
		"state-token",
		oauth2.AccessTypeOffline,
	)
	fmt.Printf("以下のURLにアクセスしてアカウントの認証を行ってください。\n"+
		"%v\n", authURL)

	authCode := <-pipe

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("トークンの取得に失敗しました: %v", err)
	}
	return tok
}

func main() {
	ctx := context.Background()
	// Create a new client with the default configuration
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GENAI_API_KEY")))
	if err != nil {
		panic(err)
	}

	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("認証情報ファイル(credentials.json)の読み込みに失敗しました: %v", err)
	}

	config, err := google.ConfigFromJSON(b, calendar.CalendarScope)
	if err != nil {
		log.Fatalf("OAuth2クライアント設定の解析に失敗しました: %v", err)
	}

	tok := getTokenFromWeb(config)
	clientOAuth := config.Client(context.Background(), tok)

	// client2, err := calendar.NewService(
	// 	ctx,
	// 	option.WithAPIKey(os.Getenv("GOOGLE_CALENDAR_API_KEY")),
	// 	option.WithScopes(calendar.CalendarScope),
	// )
	client2, err := calendar.NewService(ctx, option.WithHTTPClient(clientOAuth))
	if err != nil {
		panic(fmt.Errorf("failed to create calendar client: %w", err))
	}
	calClient = client2

	// model := client.GenerativeModel("gemma-3-12b-it")
	model := client.GenerativeModel("gemini-2.0-flash")
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("calendar_event_register 関数に日時を渡すときは、必ず RFC3339 形式で指定してください。"),
		},
		Role: "秘書",
	}
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
	reader, err := readline.NewEx(&readline.Config{
		Prompt: "Gemini> ",
	})
	if err != nil {
		panic(err)
	}
	defer reader.Close()

	for {
		prompt, err := reader.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Println("\nExiting...")
				break
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		resp, err := session.SendMessage(ctx, genai.Text(prompt))
		if err != nil {
			panic(err)
		}
		if err := consumeResponse(session, resp); err != nil {
			panic(err)
		}
	}
	fmt.Println("Session ended")
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
			return nil, fmt.Errorf("argument has no member 'count': got %v", call.Args)
		}
		if _, ok := call.Args["count"].(float64); !ok {
			return nil, fmt.Errorf("invalid argument type for 'count': expected int64, got %T", call.Args["count"])
		}

		events, err := callEventList(int64(call.Args["count"].(float64)))
		var reply genai.FunctionResponse
		if err != nil {
			log.Printf("Failed to list events: %v\n", err)
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
		if len(call.Args) < 3 {
			return nil, fmt.Errorf("missing required arguments for function call: %s; %v", call.Name, call.Args)
		}
		// FIXME: Validate the argument types...
		description, ok := call.Args["description"].(string)
		if !ok {
			description = ""
		}

		createdEventLink, err := callEventRegister(
			call.Args["start"].(string),
			call.Args["end"].(string),
			call.Args["summary"].(string),
			description,
		)
		var reply genai.FunctionResponse
		if err != nil {
			log.Printf("Failed to register event: %v\n", err)
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
					"description": description,
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

func callEventList(n int64) (map[string]any, error) {
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
	eventMap := make(map[string]any)

	for _, item := range events.Items {
		event := map[string]any{
			"id":          item.Id,
			"summary":     item.Summary,
			"description": item.Description,
			"start":       item.Start.DateTime,
			"end":         item.End.DateTime,
		}
		eventMap[item.Id] = event
	}
	return eventMap, nil
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
