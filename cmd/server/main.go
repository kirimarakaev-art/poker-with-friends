package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(server.Options{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
