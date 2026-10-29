package server

import (
	"context"
	"crypto/sha256"
	"log"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

// Store keeps accounts, rooms and hands. *store.Postgres and *store.Memory
// implement it; see package store for what each method does.
type Store interface {
	CreateUser(ctx context.Context, u store.NewUser) (store.User, error)
	UserByName(ctx context.Context, username string) (store.User, error)
	UserByID(ctx context.Context, id int64) (store.User, error)
	CreateSession(ctx context.Context, tokenHash [32]byte, userID int64, expires time.Time) error
	SessionUser(ctx context.Context, tokenHash [32]byte, now time.Time) (store.User, error)
	DeleteSession(ctx context.Context, tokenHash [32]byte) error
	ClaimBonus(ctx context.Context, userID int64, now time.Time) (store.User, error)
	UserStats(ctx context.Context, userID int64) (store.Stats, error)
	Users(ctx context.Context) ([]store.User, error)
	SetBalance(ctx context.Context, userID int64, balance int) (store.User, error)
	SetAdmin(ctx context.Context, username string, admin bool) error

	CreateRoom(ctx context.Context, r store.Room) error
	SitDown(ctx context.Context, code string, p store.Player) (balance int, err error)
	AddChips(ctx context.Context, code string, seat int, userID int64, amount int) (balance int, err error)
	SetStack(ctx context.Context, code string, seat, stack int) error
	StandUp(ctx context.Context, code string, seat, stack int) error
	SaveHand(ctx context.Context, code string, h store.Hand) error
	DeleteRoom(ctx context.Context, code string) error
	LoadRooms(ctx context.Context) ([]store.Room, error)
	Hands(ctx context.Context, code string, limit int) ([]store.Hand, error)
}

const (
	recorderQueue   = 1024
	recorderTimeout = 5 * time.Second
)

// recorder writes to the Store on its own goroutine, one job at a time and
// in order, so that a table never waits for the database.
type recorder struct {
	store Store
	jobs  chan func(context.Context, Store) error
}

func newRecorder(s Store) *recorder {
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
	r.jobs <- job
}

// wait runs job after every job recorded before it and returns its error,
// which, unlike for record, is not logged.
func (r *recorder) wait(job func(context.Context, Store) error) error {
	done := make(chan error, 1)
	r.record(func(ctx context.Context, s Store) error {
		done <- job(ctx, s)
		return nil
	})
	return <-done
}

// flush waits until every job recorded before it is done.
func (r *recorder) flush() {
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
