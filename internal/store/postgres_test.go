package store

import (
	"context"
	"crypto/rand"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// testSchemaURL creates a fresh schema in TEST_DATABASE_URL, drops it after
// the test and returns a URL that uses it. Without TEST_DATABASE_URL the
// test is skipped.
func testSchemaURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	schema := "test_" + strings.ToLower(rand.Text())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if c, err := pgx.Connect(ctx, base); err == nil {
			c.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			c.Close(ctx)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// openTest opens a store in a fresh schema of TEST_DATABASE_URL.
func openTest(t *testing.T) *Postgres {
	t.Helper()
	u := testSchemaURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, u)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestPostgres(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	runStoreSuite(t, func(t *testing.T) storeUnderTest { return openTest(t) })
}

func TestOpenTwiceKeepsData(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)
	u := mustUser(t, s, "Ann")
	mustRoom(t, s, "AAAAAA", u)
	// Migrations that are already applied must not run again.
	if err := migrate(ctx, s.pool); err != nil {
		t.Fatal(err)
	}
	rooms, err := s.LoadRooms(ctx)
	if err != nil || len(rooms) != 1 {
		t.Errorf("LoadRooms after second migrate = %v, %v", rooms, err)
	}
}

// TestMigrateFromAnonymousRooms applies the accounts migration to a
// database with rooms of the first schema: they are dropped, since they
// have no owners.
func TestMigrateFromAnonymousRooms(t *testing.T) {
	u := testSchemaURL(t)
	ctx := testCtx(t)
	pool, err := pgxpool.New(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO rooms (code, small_blind, big_blind, stack) VALUES ('AAAAAA', 5, 10, 1000)`,
		`INSERT INTO players (room_code, seat, name, token_hash, stack) VALUES ('AAAAAA', 0, 'Ann', '\x00', 1000)`,
		`INSERT INTO hands (room_code, number, board, pot, finished_at) VALUES ('AAAAAA', 1, '{}', 15, now())`,
		`INSERT INTO hand_seats (hand_id, seat, name, bet, won, stack)
		 SELECT id, 0, 'Ann', 5, 0, 995 FROM hands`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	s, err := Open(ctx, u)
	if err != nil {
		t.Fatalf("migrate old data: %v", err)
	}
	defer s.Close()
	rooms, err := s.LoadRooms(ctx)
	if err != nil || len(rooms) != 0 {
		t.Errorf("rooms after migration = %+v, %v; want none", rooms, err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM hand_seats`).Scan(&n); err != nil || n != 0 {
		t.Errorf("hand seats after migration = %d, %v; want 0", n, err)
	}

	// And back: the down migration restores the first schema.
	if _, err := p.DownTo(ctx, 1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO rooms (code, small_blind, big_blind, stack) VALUES ('BBBBBB', 5, 10, 1000)`); err != nil {
		t.Errorf("old schema after down migration: %v", err)
	}
}
