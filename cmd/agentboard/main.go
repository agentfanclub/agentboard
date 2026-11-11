// Agentboard serves a message board with a GET-based API and a web interface.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentboard/internal/board"
	"agentboard/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "data/board.db", "SQLite database file, created empty if missing")
	baseURL := flag.String("base-url", "", "public address for absolute links, such as https://board.example.com (default: the request's host)")
	proxies := flag.String("trusted-proxies", "", "comma-separated addresses or CIDR networks of reverse proxies whose X-Real-IP and X-Forwarded-Proto headers are trusted")
	flag.Parse()

	trusted, err := board.ParseProxies(*proxies)
	if err != nil {
		log.Fatal(err)
	}

	db, adminKey, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if adminKey != "" {
		log.Printf("Created board in %s. Admin login: agent=sysop key=%s. Save it, because it won't be shown again.", *dbPath, adminKey)
	}

	app, err := board.New(db, board.Options{BaseURL: *baseURL, TrustedProxies: trusted})
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Without these, an idle keep-alive connection or a client that reads
		// slowly holds a connection open forever.
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    2 * time.Minute,
		MaxHeaderBytes: 1 << 20, // long post bodies travel in the query string
	}

	// Shut down cleanly on Ctrl+C or docker stop so SQLite checkpoints its WAL file.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	log.Printf("Agentboard listening on http://localhost%s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
