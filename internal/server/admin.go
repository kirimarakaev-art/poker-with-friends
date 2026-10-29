package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

var errNotAdmin = errors.New("admins only")

// adminRoutes adds the admin API. It is for users with the admin role,
// which the account named in Options.AdminUsername gets.
func (l *lobby) adminRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/admin/rooms", l.adminOnly(l.handleAdminRooms))
	mux.Handle("POST /api/admin/rooms/{code}/kick", l.adminOnly(l.handleAdminKick))
	mux.Handle("DELETE /api/admin/rooms/{code}", l.adminOnly(l.handleAdminClose))
	mux.Handle("GET /api/admin/users", l.adminOnly(l.handleAdminUsers))
	mux.Handle("POST /api/admin/users/{id}/balance", l.adminOnly(l.handleAdminBalance))
}

// adminOnly answers 401 without a session and 403 for a user who is not an admin.
func (l *lobby) adminOnly(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := l.requireUser(w, r)
		if !ok {
			return
		}
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, errNotAdmin)
			return
		}
		next(w, r)
	})
}

// grantAdmin gives the admin role to Options.AdminUsername. It runs only at
// start, never on registration: otherwise whoever registers that name first,
// for example right after a deploy, would become admin.
func (l *lobby) grantAdmin(ctx context.Context) error {
	if l.opts.AdminUsername == "" {
		return nil
	}
	err := l.opts.Store.SetAdmin(ctx, l.opts.AdminUsername, true)
	if errors.Is(err, store.ErrNotFound) {
		log.Printf("admin: no account %q yet; register it and restart the server", l.opts.AdminUsername)
		return nil
	}
	return err
}

const maxBalance = 1_000_000_000

// adminRoomView is one room in GET /api/admin/rooms.
type adminRoomView struct {
	Code       string             `json:"code"`
	SmallBlind int                `json:"smallBlind"`
	BigBlind   int                `json:"bigBlind"`
	Hand       int                `json:"hand"`
	Street     string             `json:"street"`
	Players    []*adminPlayerView `json:"players"` // by seat, null is a free seat
}

type adminPlayerView struct {
	Seat      int    `json:"seat"`
	Name      string `json:"name"`
	Stack     int    `json:"stack"`
	Bet       int    `json:"bet"`
	Connected bool   `json:"connected"`
	InHand    bool   `json:"inHand"`
	Leaving   bool   `json:"leaving"`
}

// handleAdminRooms: GET /api/admin/rooms -> {"rooms":[...]}, sorted by code.
func (l *lobby) handleAdminRooms(w http.ResponseWriter, _ *http.Request) {
	tables := l.tables()
	rooms := make([]adminRoomView, len(tables))
	for i, t := range tables {
		rooms[i] = t.adminView()
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

type seatRequest struct {
	Seat int `json:"seat"`
}

// handleAdminKick: POST /api/admin/rooms/{code}/kick {"seat":0} frees the seat
// like a "leave" message from that player.
func (l *lobby) handleAdminKick(w http.ResponseWriter, r *http.Request) {
	t, req, ok := l.adminSeatRequest(w, r)
	if !ok {
		return
	}
	if err := t.kick(req.Seat); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (l *lobby) adminSeatRequest(w http.ResponseWriter, r *http.Request) (*table, seatRequest, bool) {
	var req seatRequest
	t := l.room(r.PathValue("code"))
	if t == nil {
		writeError(w, http.StatusNotFound, errRoomNotFound)
		return nil, req, false
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return nil, req, false
	}
	return t, req, true
}

// handleAdminClose: DELETE /api/admin/rooms/{code} disconnects everyone with
// CloseRoomNotFound and deletes the room with its history.
func (l *lobby) handleAdminClose(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(r.PathValue("code"))
	l.mu.Lock()
	t := l.rooms[code]
	delete(l.rooms, code)
	if t != nil {
		// Recorded under the lock, like CreateRoom, so it keeps its order.
		l.rec.record(func(ctx context.Context, s Store) error { return s.DeleteRoom(ctx, code) })
	}
	l.mu.Unlock()
	if t == nil {
		writeError(w, http.StatusNotFound, errRoomNotFound)
		return
	}
	t.close()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminUsers: GET /api/admin/users -> {"users":[...]}, by username.
func (l *lobby) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	users, err := l.opts.Store.Users(ctx)
	if err != nil {
		l.internalError(w, "users", err)
		return
	}
	out := make([]userView, len(users))
	now := time.Now()
	for i, u := range users {
		out[i] = userView{ID: u.ID, Username: u.Username, Balance: u.Balance, IsAdmin: u.IsAdmin, BonusAvailable: u.BonusAvailable(now)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// handleAdminBalance: POST /api/admin/users/{id}/balance {"balance":5000} -> the user.
func (l *lobby) handleAdminBalance(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	var req struct {
		Balance int `json:"balance"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Balance < 0 || req.Balance > maxBalance {
		writeError(w, http.StatusBadRequest, errors.New("balance must be 0 to 1000000000"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	u, err := l.opts.Store.SetBalance(ctx, id, req.Balance)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	if err != nil {
		l.internalError(w, "set balance", err)
		return
	}
	l.writeUser(ctx, w, u, http.StatusOK)
}
