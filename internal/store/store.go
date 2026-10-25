// Package store keeps rooms, players and finished hands in PostgreSQL,
// so that rooms survive a server restart and hand history can be read.
package store

import "time"

// Room is a table and its settings. Players and LastHand are filled
// only by LoadRooms.
type Room struct {
	Code       string
	SmallBlind int
	BigBlind   int
	Stack      int // starting stack of every player
	Players    []Player
	LastHand   int // number of the last saved hand, 0 if none
}

// Player sits at a room. Only the SHA-256 of the token is stored.
type Player struct {
	Seat      int
	Name      string
	TokenHash [32]byte
	Stack     int
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
	Seat  int
	Name  string
	Cards []string
	Bet   int // chips put in during the whole hand
	Won   int
	Stack int // after the hand
}
