// Package store keeps users, rooms, players and finished hands, so that
// accounts and rooms survive a server restart and hand history can be read.
// Postgres is the real store; Memory keeps the same data in memory for
// development without a database and for tests.
//
// Both implement:
//
//	CreateUser(ctx, NewUser) (User, error)          // ErrUsernameTaken
//	UserByName(ctx, username string) (User, error)  // case-insensitive; ErrNotFound
//	UserByID(ctx, id int64) (User, error)           // ErrNotFound
//	CreateSession(ctx, tokenHash [32]byte, userID int64, expires time.Time) error
//	SessionUser(ctx, tokenHash [32]byte, now time.Time) (User, error) // ErrNotFound if missing or expired
//	DeleteSession(ctx, tokenHash [32]byte) error
//	ClaimBonus(ctx, userID int64, now time.Time) (User, error)       // ErrBonusUnavailable
//	UserStats(ctx, userID int64) (Stats, error)
//	Users(ctx) ([]User, error)                                 // by username in any case
//	SetBalance(ctx, userID int64, balance int) (User, error)   // ErrNotFound
//	SetAdmin(ctx, username string, admin bool) error           // ErrNotFound
//
//	CreateRoom(ctx, Room) error
//	SitDown(ctx, code string, p Player) (balance int, err error) // moves p.Stack from the balance to the seat; ErrNotEnoughChips
//	AddChips(ctx, code string, seat int, userID int64, amount int) (balance int, err error) // from the balance to the user's seat
//	StandUp(ctx, code string, seat, stack int) error            // frees the seat, stack goes back to the balance
//	SetStack(ctx, code string, seat, stack int) error
//	SaveHand(ctx, code string, h Hand) error                    // also stores each seat's stack after the hand
//	DeleteRoom(ctx, code string) error                          // seated players get their saved stacks back
//	LoadRooms(ctx) ([]Room, error)
//	Hands(ctx, code string, limit int) ([]Hand, error)          // newest first
package store

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrUsernameTaken    = errors.New("username is taken")
	ErrNotEnoughChips   = errors.New("not enough chips")
	ErrBonusUnavailable = errors.New("bonus is not available")
)

// Chips a new account starts with, and the daily bonus: BonusAmount chips
// when the balance is below BonusBelow and the last bonus was at least
// BonusEvery ago.
const (
	StartBalance = 10_000
	BonusAmount  = 1_000
	BonusBelow   = 1_000
	BonusEvery   = 24 * time.Hour
)

// NewUser is an account to create. PasswordHash is a bcrypt hash.
type NewUser struct {
	Username     string
	PasswordHash string
}

// User is an account. Balance is the chips outside of tables.
type User struct {
	ID           int64
	Username     string // as typed at registration
	PasswordHash string
	Balance      int
	IsAdmin      bool
	BonusAt      time.Time // last bonus, zero if never
}

// BonusAvailable reports whether ClaimBonus would succeed at now.
func (u User) BonusAvailable(now time.Time) bool {
	return u.Balance < BonusBelow && (u.BonusAt.IsZero() || !now.Before(u.BonusAt.Add(BonusEvery)))
}

// UsernameKey is the form usernames are compared in: "Kerim" and "kerim"
// are the same account.
func UsernameKey(username string) string {
	return strings.ToLower(username)
}

// Stats counts a user's finished hands and the hands where they won chips.
type Stats struct {
	Hands    int
	HandsWon int
}

// Room is a table and its settings. Players and LastHand are filled
// only by LoadRooms.
type Room struct {
	Code       string
	OwnerID    int64
	Owner      string // owner's username, filled by LoadRooms
	Public     bool   // listed in the lobby; private rooms are joined by code
	SmallBlind int
	BigBlind   int
	MinBuyIn   int
	MaxBuyIn   int
	Players    []Player
	LastHand   int // number of the last saved hand, 0 if none
}

// Player sits at a room.
type Player struct {
	Seat   int
	UserID int64
	Name   string // username
	Stack  int
}

// Hand is a finished hand.
type Hand struct {
	Number     int
	Board      []string
	Pot        int
	FinishedAt time.Time
	Seats      []HandSeat
}

// HandSeat is one player's part in a hand. Cards are set only for hands
// shown at a showdown.
type HandSeat struct {
	Seat   int
	UserID int64
	Name   string
	Cards  []string
	Bet    int // chips put in during the whole hand
	Won    int
	Stack  int // after the hand
}
