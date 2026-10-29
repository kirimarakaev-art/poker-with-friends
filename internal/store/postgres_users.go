package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Postgres error codes.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

func isPgError(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

const userColumns = `id, username, password_hash, balance, is_admin, bonus_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	var bonusAt *time.Time
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Balance, &u.IsAdmin, &bonusAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	if bonusAt != nil {
		u.BonusAt = *bonusAt
	}
	return u, nil
}

// CreateUser creates an account with StartBalance chips.
func (s *Postgres) CreateUser(ctx context.Context, nu NewUser) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`INSERT INTO users (username, username_key, password_hash, balance)
		 VALUES ($1, $2, $3, $4) RETURNING `+userColumns,
		nu.Username, UsernameKey(nu.Username), nu.PasswordHash, StartBalance))
	if isPgError(err, pgUniqueViolation) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("create user %s: %w", nu.Username, err)
	}
	return u, nil
}

// UserByName finds a user by username in any case.
func (s *Postgres) UserByName(ctx context.Context, username string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE username_key = $1`, UsernameKey(username)))
	if err != nil {
		return User{}, fmt.Errorf("user %s: %w", username, err)
	}
	return u, nil
}

// UserByID finds a user by ID.
func (s *Postgres) UserByID(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if err != nil {
		return User{}, fmt.Errorf("user %d: %w", id, err)
	}
	return u, nil
}

// CreateSession saves a session of the user that ends at expires.
func (s *Postgres) CreateSession(ctx context.Context, tokenHash [32]byte, userID int64, expires time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash[:], userID, expires)
	if isPgError(err, pgForeignKeyViolation) {
		err = ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("create session of user %d: %w", userID, err)
	}
	return nil
}

// SessionUser returns the user of a session that has not expired at now.
// An expired session is deleted.
func (s *Postgres) SessionUser(ctx context.Context, tokenHash [32]byte, now time.Time) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT u.id, u.username, u.password_hash, u.balance, u.is_admin, u.bonus_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1 AND s.expires_at > $2`,
		tokenHash[:], now))
	if errors.Is(err, ErrNotFound) {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM sessions WHERE token_hash = $1 AND expires_at <= $2`, tokenHash[:], now); err != nil {
			return User{}, fmt.Errorf("delete expired session: %w", err)
		}
		return User{}, fmt.Errorf("session: %w", ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("session: %w", err)
	}
	return u, nil
}

// DeleteSession ends a session. A missing session is not an error.
func (s *Postgres) DeleteSession(ctx context.Context, tokenHash [32]byte) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash[:]); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ClaimBonus adds BonusAmount chips if User.BonusAvailable allows it at now.
func (s *Postgres) ClaimBonus(ctx context.Context, userID int64, now time.Time) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`UPDATE users SET balance = balance + $2, bonus_at = $3
		 WHERE id = $1 AND balance < $4 AND (bonus_at IS NULL OR bonus_at <= $5)
		 RETURNING `+userColumns,
		userID, BonusAmount, now, BonusBelow, now.Add(-BonusEvery)))
	if errors.Is(err, ErrNotFound) {
		if _, err := s.UserByID(ctx, userID); err != nil {
			return User{}, fmt.Errorf("bonus: %w", err)
		}
		return User{}, ErrBonusUnavailable
	}
	if err != nil {
		return User{}, fmt.Errorf("bonus of user %d: %w", userID, err)
	}
	return u, nil
}

// UserStats counts the user's saved hands and the hands where they won chips.
func (s *Postgres) UserStats(ctx context.Context, userID int64) (Stats, error) {
	var st Stats
	err := s.pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE won > 0) FROM hand_seats WHERE user_id = $1`,
		userID).Scan(&st.Hands, &st.HandsWon)
	if err != nil {
		return Stats{}, fmt.Errorf("stats of user %d: %w", userID, err)
	}
	return st, nil
}

// Users returns every account, sorted by username in any case.
func (s *Postgres) Users(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY username_key`)
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	return users, nil
}

// SetBalance sets a user's balance.
func (s *Postgres) SetBalance(ctx context.Context, userID int64, balance int) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`UPDATE users SET balance = $2 WHERE id = $1 RETURNING `+userColumns, userID, balance))
	if err != nil {
		return User{}, fmt.Errorf("set balance of user %d: %w", userID, err)
	}
	return u, nil
}

// SetAdmin gives or takes the admin role.
func (s *Postgres) SetAdmin(ctx context.Context, username string, admin bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET is_admin = $2 WHERE username_key = $1`, UsernameKey(username), admin)
	if err != nil {
		return fmt.Errorf("set admin %s: %w", username, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s: %w", username, ErrNotFound)
	}
	return nil
}
