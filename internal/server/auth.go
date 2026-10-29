package server

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"runtime"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie     = "session"
	sessionTTL        = 30 * 24 * time.Hour
	minUsernameLen    = 3
	maxUsernameLen    = 20
	minPasswordLen    = 8
	maxPasswordLen    = 72 // bcrypt ignores the rest
	maxAuthFailures   = 10
	loginWindow       = time.Minute
	maxSignups        = 10 // per address and signupWindow; friends may share one Wi-Fi
	signupWindow      = time.Hour
	storeTimeout      = 5 * time.Second
	errTextLogInFirst = "log in first"
)

// bcryptCost is lowered by tests; bcrypt at the default cost is slow on purpose.
var bcryptCost = bcrypt.DefaultCost

var (
	errBadUsername = errors.New("username must be 3 to 20 characters: letters, digits, dots, dashes, underscores")
	errBadPassword = errors.New("password must be 8 to 72 bytes")
	errWrongLogin  = errors.New("wrong username or password")
	errLogInFirst  = errors.New(errTextLogInFirst)
	errTooMany     = errors.New("too many attempts, try again in a minute")
	errTooManyNew  = errors.New("too many new accounts from your network, try again later")
)

// hashSlots lets only a few bcrypt runs go at once: each takes tens of
// milliseconds of CPU, and a burst of logins must not starve the tables.
var hashSlots = make(chan struct{}, max(1, runtime.NumCPU()/2))

// withHashSlot runs f when a slot is free, or gives up when ctx ends.
func withHashSlot(ctx context.Context, f func()) error {
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
		f()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dummyHash is compared against when the username does not exist, so a
// wrong username takes as long as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not a real password"), bcryptCost)

// userView is a user in the API; see docs/API.md.
type userView struct {
	ID             int64     `json:"id"`
	Username       string    `json:"username"`
	Balance        int       `json:"balance"`
	IsAdmin        bool      `json:"isAdmin"`
	BonusAvailable bool      `json:"bonusAvailable"`
	Stats          statsView `json:"stats"`
}

type statsView struct {
	Hands    int `json:"hands"`
	HandsWon int `json:"handsWon"`
}

func validUsername(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < minUsernameLen || n > maxUsernameLen {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleRegister: POST /api/auth/register {"username","password"} -> 201 User and the session cookie.
func (l *lobby) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !validUsername(req.Username) {
		writeError(w, http.StatusBadRequest, errBadUsername)
		return
	}
	if len(req.Password) < minPasswordLen || len(req.Password) > maxPasswordLen {
		writeError(w, http.StatusBadRequest, errBadPassword)
		return
	}
	ip, now := clientIP(r), time.Now()
	if l.auth.blocked(ip, now) {
		writeError(w, http.StatusTooManyRequests, errTooMany)
		return
	}
	if l.signups.blocked(ip, now) {
		writeError(w, http.StatusTooManyRequests, errTooManyNew)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	var hash []byte
	var hashErr error
	err := withHashSlot(ctx, func() { hash, hashErr = bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost) })
	if err == nil {
		err = hashErr
	}
	if err != nil {
		l.internalError(w, "hash password", err)
		return
	}
	u, err := l.opts.Store.CreateUser(ctx, store.NewUser{Username: req.Username, PasswordHash: string(hash)})
	if errors.Is(err, store.ErrUsernameTaken) {
		l.auth.add(ip, now) // also slows down probing for taken names
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		l.internalError(w, "create user", err)
		return
	}
	l.signups.add(ip, now)
	l.startSession(ctx, w, r, u, http.StatusCreated)
}

// handleLogin: POST /api/auth/login {"username","password"} -> 200 User and the session cookie.
func (l *lobby) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ip, now := clientIP(r), time.Now()
	if l.auth.blocked(ip, now) {
		writeError(w, http.StatusTooManyRequests, errTooMany)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	u, err := l.opts.Store.UserByName(ctx, req.Username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		l.internalError(w, "find user", err)
		return
	}
	hash := dummyHash
	if err == nil {
		hash = []byte(u.PasswordHash)
	}
	var wrong error
	if slotErr := withHashSlot(ctx, func() { wrong = bcrypt.CompareHashAndPassword(hash, []byte(req.Password)) }); slotErr != nil {
		l.internalError(w, "check password", slotErr)
		return
	}
	if wrong != nil || err != nil {
		l.auth.add(ip, now)
		writeError(w, http.StatusUnauthorized, errWrongLogin)
		return
	}
	l.startSession(ctx, w, r, u, http.StatusOK)
}

func (l *lobby) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, u store.User, status int) {
	token := rand.Text()
	if err := l.opts.Store.CreateSession(ctx, hashToken(token), u.ID, time.Now().Add(sessionTTL)); err != nil {
		l.internalError(w, "create session", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	l.writeUser(ctx, w, u, status)
}

// handleLogout: POST /api/auth/logout ends the session, if any.
func (l *lobby) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
		defer cancel()
		if err := l.opts.Store.DeleteSession(ctx, hashToken(c.Value)); err != nil {
			l.internalError(w, "delete session", err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe: GET /api/me -> User.
func (l *lobby) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := l.requireUser(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	l.writeUser(ctx, w, u, http.StatusOK)
}

// handleBonus: POST /api/me/bonus -> User with the bonus added.
func (l *lobby) handleBonus(w http.ResponseWriter, r *http.Request) {
	u, ok := l.requireUser(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	u, err := l.opts.Store.ClaimBonus(ctx, u.ID, time.Now())
	if errors.Is(err, store.ErrBonusUnavailable) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		l.internalError(w, "claim bonus", err)
		return
	}
	l.writeUser(ctx, w, u, http.StatusOK)
}

func (l *lobby) writeUser(ctx context.Context, w http.ResponseWriter, u store.User, status int) {
	stats, err := l.opts.Store.UserStats(ctx, u.ID)
	if err != nil {
		l.internalError(w, "user stats", err)
		return
	}
	writeJSON(w, status, userView{
		ID:             u.ID,
		Username:       u.Username,
		Balance:        u.Balance,
		IsAdmin:        u.IsAdmin,
		BonusAvailable: u.BonusAvailable(time.Now()),
		Stats:          statsView{Hands: stats.Hands, HandsWon: stats.HandsWon},
	})
}

// currentUser is the user of the request's session cookie.
func (l *lobby) currentUser(r *http.Request) (store.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, errLogInFirst
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	u, err := l.opts.Store.SessionUser(ctx, hashToken(c.Value), time.Now())
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, errLogInFirst
	}
	return u, err
}

// requireUser writes 401 (or 500) and returns false when nobody is logged in.
func (l *lobby) requireUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	u, err := l.currentUser(r)
	if errors.Is(err, errLogInFirst) {
		writeError(w, http.StatusUnauthorized, err)
		return u, false
	}
	if err != nil {
		l.internalError(w, "session", err)
		return u, false
	}
	return u, true
}

func (l *lobby) internalError(w http.ResponseWriter, what string, err error) {
	log.Printf("%s: %v", what, err)
	writeError(w, http.StatusInternalServerError, errors.New("internal error"))
}
