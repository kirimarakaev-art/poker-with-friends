package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Postgres is the store backed by a PostgreSQL connection pool.
type Postgres struct {
	pool *pgxpool.Pool
}

// Open connects to url and applies pending migrations.
func Open(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, dir)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Close closes all connections.
func (s *Postgres) Close() {
	s.pool.Close()
}

// CreateRoom saves a new room.
func (s *Postgres) CreateRoom(ctx context.Context, r Room) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO rooms (code, small_blind, big_blind, stack) VALUES ($1, $2, $3, $4)`,
		r.Code, r.SmallBlind, r.BigBlind, r.Stack)
	if err != nil {
		return fmt.Errorf("create room %s: %w", r.Code, err)
	}
	return nil
}

// AddPlayer seats a player at the room.
func (s *Postgres) AddPlayer(ctx context.Context, code string, p Player) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO players (room_code, seat, name, token_hash, stack) VALUES ($1, $2, $3, $4, $5)`,
		code, p.Seat, p.Name, p.TokenHash[:], p.Stack)
	if err != nil {
		return fmt.Errorf("add player %s/%d: %w", code, p.Seat, err)
	}
	return nil
}

// SaveHand saves a finished hand and the players' new stacks in one transaction.
func (s *Postgres) SaveHand(ctx context.Context, code string, h Hand) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx,
			`INSERT INTO hands (room_code, number, board, pot, finished_at)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			code, h.Number, h.Board, h.Pot, h.FinishedAt).Scan(&id)
		if err != nil {
			return err
		}
		var batch pgx.Batch
		for _, hs := range h.Seats {
			batch.Queue(`INSERT INTO hand_seats (hand_id, seat, name, cards, bet, won, stack)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				id, hs.Seat, hs.Name, hs.Cards, hs.Bet, hs.Won, hs.Stack)
			batch.Queue(`UPDATE players SET stack = $3 WHERE room_code = $1 AND seat = $2`,
				code, hs.Seat, hs.Stack)
		}
		return tx.SendBatch(ctx, &batch).Close()
	})
	if err != nil {
		return fmt.Errorf("save hand %s/%d: %w", code, h.Number, err)
	}
	return nil
}

// LoadRooms returns every room with its players and last hand number.
func (s *Postgres) LoadRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT r.code, r.small_blind, r.big_blind, r.stack,
		        COALESCE((SELECT max(number) FROM hands h WHERE h.room_code = r.code), 0)
		 FROM rooms r ORDER BY r.code`)
	if err != nil {
		return nil, fmt.Errorf("load rooms: %w", err)
	}
	rooms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Room, error) {
		var r Room
		err := row.Scan(&r.Code, &r.SmallBlind, &r.BigBlind, &r.Stack, &r.LastHand)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("load rooms: %w", err)
	}
	byCode := make(map[string]*Room, len(rooms))
	for i := range rooms {
		byCode[rooms[i].Code] = &rooms[i]
	}

	rows, err = s.pool.Query(ctx,
		`SELECT room_code, seat, name, token_hash, stack FROM players ORDER BY room_code, seat`)
	if err != nil {
		return nil, fmt.Errorf("load players: %w", err)
	}
	var code string
	var p Player
	var hash []byte
	_, err = pgx.ForEachRow(rows, []any{&code, &p.Seat, &p.Name, &hash, &p.Stack}, func() error {
		if len(hash) != len(p.TokenHash) {
			return errors.New("bad token hash length")
		}
		copy(p.TokenHash[:], hash)
		if r := byCode[code]; r != nil {
			r.Players = append(r.Players, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load players: %w", err)
	}
	return rooms, nil
}

// Hands returns up to limit last hands of the room, newest first.
func (s *Postgres) Hands(ctx context.Context, code string, limit int) ([]Hand, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, number, board, pot, finished_at FROM hands
		 WHERE room_code = $1 ORDER BY number DESC LIMIT $2`,
		code, limit)
	if err != nil {
		return nil, fmt.Errorf("load hands: %w", err)
	}
	var ids []int64
	hands, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Hand, error) {
		var id int64
		var h Hand
		err := row.Scan(&id, &h.Number, &h.Board, &h.Pot, &h.FinishedAt)
		ids = append(ids, id)
		return h, err
	})
	if err != nil {
		return nil, fmt.Errorf("load hands: %w", err)
	}
	index := make(map[int64]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}

	rows, err = s.pool.Query(ctx,
		`SELECT hand_id, seat, name, cards, bet, won, stack FROM hand_seats
		 WHERE hand_id = ANY($1) ORDER BY hand_id, seat`,
		ids)
	if err != nil {
		return nil, fmt.Errorf("load hand seats: %w", err)
	}
	var id int64
	var hs HandSeat
	_, err = pgx.ForEachRow(rows, []any{&id, &hs.Seat, &hs.Name, &hs.Cards, &hs.Bet, &hs.Won, &hs.Stack}, func() error {
		h := &hands[index[id]]
		h.Seats = append(h.Seats, hs)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load hand seats: %w", err)
	}
	return hands, nil
}
