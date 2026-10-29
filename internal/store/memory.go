package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory keeps the same data as Postgres in memory, with the same rules and
// errors. It is safe for concurrent use. Results never share slices with
// the store.
type Memory struct {
	mu         sync.Mutex
	nextUserID int64
	users      map[int64]User
	userKeys   map[string]int64
	sessions   map[[32]byte]memSession
	rooms      map[string]*memRoom
}

type memSession struct {
	userID  int64
	expires time.Time
}

type memRoom struct {
	room    Room           // without Players and LastHand
	players map[int]Player // by seat
	hands   []Hand         // by number, oldest first
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{
		users:    make(map[int64]User),
		userKeys: make(map[string]int64),
		sessions: make(map[[32]byte]memSession),
		rooms:    make(map[string]*memRoom),
	}
}

// dbTime rounds t the way Postgres stores timestamps.
func dbTime(t time.Time) time.Time {
	return t.Truncate(time.Microsecond)
}

// CreateUser creates an account with StartBalance chips.
func (m *Memory) CreateUser(_ context.Context, nu NewUser) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := UsernameKey(nu.Username)
	if _, ok := m.userKeys[key]; ok {
		return User{}, ErrUsernameTaken
	}
	m.nextUserID++
	u := User{ID: m.nextUserID, Username: nu.Username, PasswordHash: nu.PasswordHash, Balance: StartBalance}
	m.users[u.ID] = u
	m.userKeys[key] = u.ID
	return u, nil
}

// UserByName finds a user by username in any case.
func (m *Memory) UserByName(_ context.Context, username string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.userKeys[UsernameKey(username)]
	if !ok {
		return User{}, fmt.Errorf("user %s: %w", username, ErrNotFound)
	}
	return m.users[id], nil
}

// UserByID finds a user by ID.
func (m *Memory) UserByID(_ context.Context, id int64) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, fmt.Errorf("user %d: %w", id, ErrNotFound)
	}
	return u, nil
}

// CreateSession saves a session of the user that ends at expires.
func (m *Memory) CreateSession(_ context.Context, tokenHash [32]byte, userID int64, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return fmt.Errorf("create session of user %d: %w", userID, ErrNotFound)
	}
	if _, ok := m.sessions[tokenHash]; ok {
		return fmt.Errorf("create session of user %d: session exists", userID)
	}
	m.sessions[tokenHash] = memSession{userID: userID, expires: dbTime(expires)}
	return nil
}

// SessionUser returns the user of a session that has not expired at now.
// An expired session is deleted.
func (m *Memory) SessionUser(_ context.Context, tokenHash [32]byte, now time.Time) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tokenHash]
	if !ok {
		return User{}, fmt.Errorf("session: %w", ErrNotFound)
	}
	if !s.expires.After(now) {
		delete(m.sessions, tokenHash)
		return User{}, fmt.Errorf("session: %w", ErrNotFound)
	}
	return m.users[s.userID], nil
}

// DeleteSession ends a session. A missing session is not an error.
func (m *Memory) DeleteSession(_ context.Context, tokenHash [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tokenHash)
	return nil
}

// ClaimBonus adds BonusAmount chips if User.BonusAvailable allows it at now.
func (m *Memory) ClaimBonus(_ context.Context, userID int64, now time.Time) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return User{}, fmt.Errorf("bonus: user %d: %w", userID, ErrNotFound)
	}
	if !u.BonusAvailable(now) {
		return User{}, ErrBonusUnavailable
	}
	u.Balance += BonusAmount
	u.BonusAt = dbTime(now)
	m.users[userID] = u
	return u, nil
}

// UserStats counts the user's saved hands and the hands where they won chips.
func (m *Memory) UserStats(_ context.Context, userID int64) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var st Stats
	for _, r := range m.rooms {
		for _, h := range r.hands {
			for _, hs := range h.Seats {
				if hs.UserID != userID {
					continue
				}
				st.Hands++
				if hs.Won > 0 {
					st.HandsWon++
				}
			}
		}
	}
	return st, nil
}

// CreateRoom saves a new room. A missing owner is ErrNotFound.
func (m *Memory) CreateRoom(_ context.Context, r Room) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[r.Code]; ok {
		return fmt.Errorf("create room %s: code is taken", r.Code)
	}
	if _, ok := m.users[r.OwnerID]; !ok {
		return fmt.Errorf("create room %s: owner: %w", r.Code, ErrNotFound)
	}
	if !(0 < r.SmallBlind && r.SmallBlind <= r.BigBlind && r.BigBlind <= r.MinBuyIn && r.MinBuyIn <= r.MaxBuyIn) {
		return fmt.Errorf("create room %s: bad blinds or buy-ins", r.Code)
	}
	r.Owner, r.Players, r.LastHand = "", nil, 0
	m.rooms[r.Code] = &memRoom{room: r, players: make(map[int]Player)}
	return nil
}

// SitDown moves p.Stack chips from the user's balance to a seat and returns
// the new balance. With fewer chips than p.Stack nothing changes and the
// error is ErrNotEnoughChips; a missing user or room is ErrNotFound.
func (m *Memory) SitDown(_ context.Context, code string, p Player) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	balance, err := m.sitDown(code, p)
	if err != nil {
		return 0, fmt.Errorf("sit down %s/%d: %w", code, p.Seat, err)
	}
	return balance, nil
}

// sitDown checks in the order Postgres does: user, chips, room, seat.
func (m *Memory) sitDown(code string, p Player) (int, error) {
	u, ok := m.users[p.UserID]
	if !ok {
		return 0, ErrNotFound
	}
	if u.Balance < p.Stack {
		return 0, ErrNotEnoughChips
	}
	r, ok := m.rooms[code]
	if !ok {
		return 0, ErrNotFound
	}
	if p.Seat < 0 || p.Stack < 0 {
		return 0, errors.New("negative seat or stack")
	}
	if _, ok := r.players[p.Seat]; ok {
		return 0, errors.New("seat is taken")
	}
	for _, other := range r.players {
		if other.UserID == p.UserID {
			return 0, errors.New("user is already seated")
		}
	}
	u.Balance -= p.Stack
	m.users[u.ID] = u
	r.players[p.Seat] = Player{Seat: p.Seat, UserID: u.ID, Name: u.Username, Stack: p.Stack}
	return u.Balance, nil
}

// StandUp frees a seat and adds stack to the player's balance. A free seat
// is ErrNotFound. The player's past hands stay in the history.
func (m *Memory) StandUp(_ context.Context, code string, seat, stack int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[code]
	if !ok {
		return fmt.Errorf("stand up %s/%d: %w", code, seat, ErrNotFound)
	}
	p, ok := r.players[seat]
	if !ok {
		return fmt.Errorf("stand up %s/%d: %w", code, seat, ErrNotFound)
	}
	u := m.users[p.UserID]
	if u.Balance+stack < 0 {
		return fmt.Errorf("stand up %s/%d: negative balance", code, seat)
	}
	u.Balance += stack
	m.users[u.ID] = u
	delete(r.players, seat)
	return nil
}

// SetStack changes a player's stack outside of a hand.
func (m *Memory) SetStack(_ context.Context, code string, seat, stack int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[code]
	if !ok {
		return nil
	}
	p, ok := r.players[seat]
	if !ok {
		return nil
	}
	if stack < 0 {
		return fmt.Errorf("set stack %s/%d: negative stack", code, seat)
	}
	p.Stack = stack
	r.players[seat] = p
	return nil
}

// SaveHand saves a finished hand and the players' new stacks. A missing room
// is ErrNotFound.
func (m *Memory) SaveHand(_ context.Context, code string, h Hand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkHand(code, h); err != nil {
		return fmt.Errorf("save hand %s/%d: %w", code, h.Number, err)
	}
	r := m.rooms[code]
	h = copyHand(h)
	if h.Board == nil {
		h.Board = []string{}
	}
	if len(h.Seats) == 0 {
		h.Seats = nil
	}
	h.FinishedAt = dbTime(h.FinishedAt)
	sort.Slice(h.Seats, func(i, j int) bool { return h.Seats[i].Seat < h.Seats[j].Seat })
	for _, hs := range h.Seats {
		if p, ok := r.players[hs.Seat]; ok {
			p.Stack = hs.Stack
			r.players[hs.Seat] = p
		}
	}
	i := sort.Search(len(r.hands), func(i int) bool { return r.hands[i].Number > h.Number })
	r.hands = slices.Insert(r.hands, i, h)
	return nil
}

// checkHand finds what Postgres constraints would reject in SaveHand.
func (m *Memory) checkHand(code string, h Hand) error {
	r, ok := m.rooms[code]
	if !ok {
		return ErrNotFound
	}
	for _, old := range r.hands {
		if old.Number == h.Number {
			return errors.New("hand number exists")
		}
	}
	seats := make(map[int]bool, len(h.Seats))
	for _, hs := range h.Seats {
		if seats[hs.Seat] {
			return errors.New("seat twice in a hand")
		}
		seats[hs.Seat] = true
		if _, ok := m.users[hs.UserID]; hs.UserID != 0 && !ok {
			return ErrNotFound
		}
		if _, ok := r.players[hs.Seat]; ok && hs.Stack < 0 {
			return errors.New("negative stack")
		}
	}
	return nil
}

// DeleteRoom returns every seated player's saved stack to their balance and
// deletes the room with its players and hand history. A missing room is not
// an error.
func (m *Memory) DeleteRoom(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[code]
	if !ok {
		return nil
	}
	for _, p := range r.players {
		u := m.users[p.UserID]
		u.Balance += p.Stack
		m.users[u.ID] = u
	}
	delete(m.rooms, code)
	return nil
}

// LoadRooms returns every room with its owner, players and last hand number.
func (m *Memory) LoadRooms(_ context.Context) ([]Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rooms := make([]Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		room := r.room
		room.Owner = m.users[room.OwnerID].Username
		if n := len(r.hands); n > 0 {
			room.LastHand = r.hands[n-1].Number
		}
		for _, p := range r.players {
			p.Name = m.users[p.UserID].Username
			room.Players = append(room.Players, p)
		}
		sort.Slice(room.Players, func(i, j int) bool { return room.Players[i].Seat < room.Players[j].Seat })
		rooms = append(rooms, room)
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].Code < rooms[j].Code })
	return rooms, nil
}

// Hands returns up to limit last hands of the room, newest first.
func (m *Memory) Hands(_ context.Context, code string, limit int) ([]Hand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 0 {
		return nil, fmt.Errorf("load hands: negative limit %d", limit)
	}
	hands := []Hand{}
	r, ok := m.rooms[code]
	if !ok {
		return hands, nil
	}
	for i := len(r.hands) - 1; i >= 0 && len(hands) < limit; i-- {
		hands = append(hands, copyHand(r.hands[i]))
	}
	return hands, nil
}

// copyHand returns h with its own slices.
func copyHand(h Hand) Hand {
	h.Board = slices.Clone(h.Board)
	h.Seats = slices.Clone(h.Seats)
	for i := range h.Seats {
		h.Seats[i].Cards = slices.Clone(h.Seats[i].Cards)
	}
	return h
}

// Users returns every account, sorted by username in any case.
func (m *Memory) Users(_ context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b User) int { return strings.Compare(UsernameKey(a.Username), UsernameKey(b.Username)) })
	return out, nil
}

// SetBalance sets a user's balance.
func (m *Memory) SetBalance(_ context.Context, userID int64, balance int) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return User{}, fmt.Errorf("user %d: %w", userID, ErrNotFound)
	}
	u.Balance = balance
	m.users[userID] = u
	return u, nil
}

// SetAdmin gives or takes the admin role.
func (m *Memory) SetAdmin(_ context.Context, username string, admin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.userKeys[UsernameKey(username)]
	if !ok {
		return fmt.Errorf("user %s: %w", username, ErrNotFound)
	}
	u := m.users[id]
	u.IsAdmin = admin
	m.users[id] = u
	return nil
}
