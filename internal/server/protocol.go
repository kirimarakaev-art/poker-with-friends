package server

import (
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/engine"
	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

// clientMsg is a message from the browser.
//
//	{"type":"act","action":"raise","amount":200}
//	{"type":"chat","text":"nice hand"}
//	{"type":"leave"}
type clientMsg struct {
	Type   string `json:"type"`
	Action string `json:"action,omitempty"`
	Amount int    `json:"amount,omitempty"`
	Text   string `json:"text,omitempty"`
}

// serverMsg is a message to the browser: "state", "chat" or "error".
type serverMsg struct {
	Type  string     `json:"type"`
	State *stateView `json:"state,omitempty"`
	From  string     `json:"from,omitempty"`
	Text  string     `json:"text,omitempty"`
	Error string     `json:"error,omitempty"`
}

var actionsByName = map[string]engine.Action{
	"fold":  engine.Fold,
	"check": engine.Check,
	"call":  engine.Call,
	"bet":   engine.Bet,
	"raise": engine.Raise,
	"allin": engine.AllIn,
}

// stateView is the table as one player sees it. Seats are table seats,
// not engine seats; -1 means nobody. A free seat is null in Players.
type stateView struct {
	Hand       int           `json:"hand"`
	Street     string        `json:"street"`
	Board      []string      `json:"board"`
	Pot        int           `json:"pot"`
	CurrentBet int           `json:"currentBet"`
	Button     int           `json:"button"`
	ToAct      int           `json:"toAct"`
	Deadline   int64         `json:"deadline,omitempty"` // unix ms
	You        int           `json:"you"`
	Players    []*playerView `json:"players"`
	Options    *optionsView  `json:"options,omitempty"`
}

type playerView struct {
	Seat      int      `json:"seat"`
	Name      string   `json:"name"`
	Stack     int      `json:"stack"`
	Bet       int      `json:"bet"`
	Cards     []string `json:"cards,omitempty"`
	HandName  string   `json:"handName,omitempty"`
	InHand    bool     `json:"inHand"`
	Folded    bool     `json:"folded"`
	AllIn     bool     `json:"allIn"`
	Connected bool     `json:"connected"`
	Won       int      `json:"won,omitempty"`
}

type optionsView struct {
	CanCheck   bool `json:"canCheck"`
	CallAmount int  `json:"callAmount"`
	CanRaise   bool `json:"canRaise"`
	MinRaiseTo int  `json:"minRaiseTo"`
	MaxRaiseTo int  `json:"maxRaiseTo"`
}

func cardStrings(cards []engine.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.String()
	}
	return out
}

// handView is a finished hand in GET /api/rooms/{code}/hands.
type handView struct {
	Hand       int            `json:"hand"`
	Board      []string       `json:"board"`
	Pot        int            `json:"pot"`
	FinishedAt time.Time      `json:"finishedAt"`
	Players    []handSeatView `json:"players"`
}

type handSeatView struct {
	Seat  int      `json:"seat"`
	Name  string   `json:"name"`
	Cards []string `json:"cards,omitempty"`
	Bet   int      `json:"bet"`
	Won   int      `json:"won"`
	Stack int      `json:"stack"`
}

func newHandView(h store.Hand) handView {
	v := handView{Hand: h.Number, Board: h.Board, Pot: h.Pot, FinishedAt: h.FinishedAt, Players: make([]handSeatView, len(h.Seats))}
	if v.Board == nil {
		v.Board = []string{}
	}
	for i, s := range h.Seats {
		v.Players[i] = handSeatView{Seat: s.Seat, Name: s.Name, Cards: s.Cards, Bet: s.Bet, Won: s.Won, Stack: s.Stack}
	}
	return v
}
