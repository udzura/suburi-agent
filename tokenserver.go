package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

var once = new(sync.Once)

func acceptTokenViaLocalHTTP(ctx context.Context, pipe chan<- string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("code")
		if token != "" {
			once.Do(func() {
				pipe <- token
			})
		}

		w.WriteHeader(http.StatusOK)
		w.Write(fmt.Appendf(nil, `
<!DOCTYPE html>
<html>
<head>
	<title>Token Server</title>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body>
	<h1>Token is:</h1>
	<pre>%s</re>
	<p>認証に成功したらこのページを閉じてください</p>
</body>
</html>
		`, token))
	}))
	server := &http.Server{
		Addr:    ":28080",
		Handler: mux,
	}
	server.RegisterOnShutdown(func() {
		close(pipe)
	})

	go func() {
		// contextからのキャンセル通知を待つ
		<-ctx.Done()

		// シャットダウン処理自体にもタイムアウトを設定
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Graceful Shutdownを実行
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error during server shutdown: %v", err)
		}
	}()

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("failed to start server: %s", err.Error())
		}
	}()
}
