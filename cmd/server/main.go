package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	static := flag.String("static", "", "directory with the built frontend (frontend/dist) to serve at /")
	flag.Parse()

	if *static != "" {
		if _, err := os.Stat(filepath.Join(*static, "index.html")); err != nil {
			log.Fatalf("static: %v", err)
		}
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(server.Options{StaticDir: *static}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
