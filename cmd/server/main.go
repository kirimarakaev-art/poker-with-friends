package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/server"
	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

const startTimeout = 30 * time.Second

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	static := flag.String("static", "", "directory with the built frontend (frontend/dist) to serve at /")
	flag.Parse()

	if *static != "" {
		if _, err := os.Stat(filepath.Join(*static, "index.html")); err != nil {
			log.Fatalf("static: %v", err)
		}
	}

	handler, err := newHandler(server.Options{StaticDir: *static}, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// newHandler keeps everything in memory when dbURL is empty; otherwise it
// connects to PostgreSQL, migrates it and restores the saved rooms.
func newHandler(opts server.Options, dbURL string) (http.Handler, error) {
	if dbURL == "" {
		log.Printf("DATABASE_URL is not set, rooms live only in memory")
		return server.New(opts), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	opts.Store = st
	return server.Open(ctx, opts)
}
