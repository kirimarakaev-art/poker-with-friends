package server

import (
	"context"
	"crypto/sha256"
	"log"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

// Store saves rooms and hands. *store.Postgres implements it.
type Store interface {
	CreateRoom(ctx context.Context, r store.Room) error
	AddPlayer(ctx context.Context, code string, p store.Player) error
	RemovePlayer(ctx context.Context, code string, seat int) error
	SetStack(ctx context.Context, code string, seat, stack int) error
	DeleteRoom(ctx context.Context, code string) error
	SaveHand(ctx context.Context, code string, h store.Hand) error
	LoadRooms(ctx context.Context) ([]store.Room, error)
	Hands(ctx context.Context, code string, limit int) ([]store.Hand, error)
}

const (
	recorderQueue   = 1024
	recorderTimeout = 5 * time.Second
)

// recorder writes to the Store on its own goroutine, one job at a time and
// in order, so that a table never waits for the database. A nil recorder
// (no Store) drops everything.
type recorder struct {
	store Store
	jobs  chan func(context.Context, Store) error
}

func newRecorder(s Store) *recorder {
	if s == nil {
		return nil
	}
	r := &recorder{store: s, jobs: make(chan func(context.Context, Store) error, recorderQueue)}
	go func() {
		for job := range r.jobs {
			ctx, cancel := context.WithTimeout(context.Background(), recorderTimeout)
			if err := job(ctx, r.store); err != nil {
				log.Printf("store: %v", err)
			}
			cancel()
		}
	}()
	return r
}

func (r *recorder) record(job func(context.Context, Store) error) {
	if r != nil {
		r.jobs <- job
	}
}

// flush waits until every job recorded before it is done.
func (r *recorder) flush() {
	if r == nil {
		return
	}
	done := make(chan struct{})
	r.record(func(context.Context, Store) error {
		close(done)
		return nil
	})
	<-done
}

func hashToken(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}
