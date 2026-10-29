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

// CreateRoom saves a new room. A missing owner is ErrNotFound.
func (s *Postgres) CreateRoom(ctx context.Context, r Room) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO rooms (code, owner_id, public, small_blind, big_blind, min_buy_in, max_buy_in)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.Code, r.OwnerID, r.Public, r.SmallBlind, r.BigBlind, r.MinBuyIn, r.MaxBuyIn)
	if isPgError(err, pgForeignKeyViolation) {
		err = ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("create room %s: %w", r.Code, err)
	}
	return nil
}

// SitDown moves p.Stack chips from the user's balance to a seat and returns
// the new balance. With fewer chips than p.Stack nothing changes and the
// error is ErrNotEnoughChips; a missing user or room is ErrNotFound.
func (s *Postgres) SitDown(ctx context.Context, code string, p Player) (int, error) {
	var balance int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var username string
		err := tx.QueryRow(ctx,
			`UPDATE users SET balance = balance - $2 WHERE id = $1 AND balance >= $2
			 RETURNING balance, username`,
			p.UserID, p.Stack).Scan(&balance, &username)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, p.UserID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			return ErrNotEnoughChips
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO players (room_code, seat, user_id, name, stack) VALUES ($1, $2, $3, $4, $5)`,
			code, p.Seat, p.UserID, username, p.Stack)
		if isPgError(err, pgForeignKeyViolation) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("sit down %s/%d: %w", code, p.Seat, err)
	}
	return balance, nil
}

// AddChips moves amount chips from the user's balance to the user's seat
// and returns the new balance. With fewer chips than amount nothing changes
// and the error is ErrNotEnoughChips; a seat that is free or someone else's
// is ErrNotFound.
func (s *Postgres) AddChips(ctx context.Context, code string, seat int, userID int64, amount int) (int, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("add chips %s/%d: amount %d is not positive", code, seat, amount)
	}
	var balance int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE players SET stack = stack + $4 WHERE room_code = $1 AND seat = $2 AND user_id = $3`,
			code, seat, userID, amount)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		err = tx.QueryRow(ctx,
			`UPDATE users SET balance = balance - $2 WHERE id = $1 AND balance >= $2 RETURNING balance`,
			userID, amount).Scan(&balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotEnoughChips
		}
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("add chips %s/%d: %w", code, seat, err)
	}
	return balance, nil
}

// StandUp frees a seat and adds stack to the player's balance. A free seat
// is ErrNotFound. The player's past hands stay in the history.
func (s *Postgres) StandUp(ctx context.Context, code string, seat, stack int) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var userID int64
		err := tx.QueryRow(ctx,
			`DELETE FROM players WHERE room_code = $1 AND seat = $2 RETURNING user_id`,
			code, seat).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET balance = balance + $2 WHERE id = $1`, userID, stack)
		return err
	})
	if err != nil {
		return fmt.Errorf("stand up %s/%d: %w", code, seat, err)
	}
	return nil
}

// SetStack changes a player's stack outside of a hand.
func (s *Postgres) SetStack(ctx context.Context, code string, seat, stack int) error {
	_, err := s.pool.Exec(ctx, `UPDATE players SET stack = $3 WHERE room_code = $1 AND seat = $2`, code, seat, stack)
	if err != nil {
		return fmt.Errorf("set stack %s/%d: %w", code, seat, err)
	}
	return nil
}

// DeleteRoom returns every seated player's saved stack to their balance and
// deletes the room with its players and hand history. A missing room is not
// an error.
func (s *Postgres) DeleteRoom(ctx context.Context, code string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE users u SET balance = u.balance + p.stack
			 FROM players p WHERE p.room_code = $1 AND p.user_id = u.id`, code)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM rooms WHERE code = $1`, code)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete room %s: %w", code, err)
	}
	return nil
}

// SaveHand saves a finished hand and the players' new stacks in one
// transaction. A missing room is ErrNotFound.
func (s *Postgres) SaveHand(ctx context.Context, code string, h Hand) error {
	board := h.Board
	if board == nil {
		board = []string{}
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx,
			`INSERT INTO hands (room_code, number, board, pot, finished_at)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			code, h.Number, board, h.Pot, h.FinishedAt).Scan(&id)
		if err != nil {
			return err
		}
		var batch pgx.Batch
		for _, hs := range h.Seats {
			batch.Queue(`INSERT INTO hand_seats (hand_id, seat, user_id, name, cards, bet, won, stack)
				VALUES ($1, $2, NULLIF($3::bigint, 0), $4, $5, $6, $7, $8)`,
				id, hs.Seat, hs.UserID, hs.Name, hs.Cards, hs.Bet, hs.Won, hs.Stack)
			batch.Queue(`UPDATE players SET stack = $3 WHERE room_code = $1 AND seat = $2`,
				code, hs.Seat, hs.Stack)
		}
		return tx.SendBatch(ctx, &batch).Close()
	})
	if isPgError(err, pgForeignKeyViolation) {
		err = ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("save hand %s/%d: %w", code, h.Number, err)
	}
	return nil
}

// LoadRooms returns every room with its owner, players and last hand number.
func (s *Postgres) LoadRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT r.code, r.owner_id, u.username, r.public, r.small_blind, r.big_blind,
		        r.min_buy_in, r.max_buy_in,
		        COALESCE((SELECT max(number) FROM hands h WHERE h.room_code = r.code), 0)
		 FROM rooms r JOIN users u ON u.id = r.owner_id ORDER BY r.code`)
	if err != nil {
		return nil, fmt.Errorf("load rooms: %w", err)
	}
	rooms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Room, error) {
		var r Room
		err := row.Scan(&r.Code, &r.OwnerID, &r.Owner, &r.Public, &r.SmallBlind, &r.BigBlind,
			&r.MinBuyIn, &r.MaxBuyIn, &r.LastHand)
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
		`SELECT p.room_code, p.seat, p.user_id, u.username, p.stack
		 FROM players p JOIN users u ON u.id = p.user_id ORDER BY p.room_code, p.seat`)
	if err != nil {
		return nil, fmt.Errorf("load players: %w", err)
	}
	var code string
	var p Player
	_, err = pgx.ForEachRow(rows, []any{&code, &p.Seat, &p.UserID, &p.Name, &p.Stack}, func() error {
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
		`SELECT hand_id, seat, COALESCE(user_id, 0), name, cards, bet, won, stack FROM hand_seats
		 WHERE hand_id = ANY($1) ORDER BY hand_id, seat`,
		ids)
	if err != nil {
		return nil, fmt.Errorf("load hand seats: %w", err)
	}
	var id int64
	var hs HandSeat
	_, err = pgx.ForEachRow(rows, []any{&id, &hs.Seat, &hs.UserID, &hs.Name, &hs.Cards, &hs.Bet, &hs.Won, &hs.Stack}, func() error {
		h := &hands[index[id]]
		h.Seats = append(h.Seats, hs)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load hand seats: %w", err)
	}
	return hands, nil
}
