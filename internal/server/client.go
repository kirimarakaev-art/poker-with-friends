package server

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const (
	sendBuffer   = 16
	writeTimeout = 5 * time.Second
	maxChatLen   = 200
	maxMsgSize   = 4 << 10
)

// CloseReplaced is the close code sent to a connection when the same player
// connects again, for example from another tab. Clients should not reconnect
// automatically after it.
const CloseReplaced websocket.StatusCode = 4001

// CloseRoomNotFound is the close code sent when the room in the URL does not
// exist, for example after a server restart. Clients should stop reconnecting.
const CloseRoomNotFound websocket.StatusCode = 4004

// client is one WebSocket connection of a seated player.
type client struct {
	conn *websocket.Conn
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func newClient(conn *websocket.Conn) *client {
	conn.SetReadLimit(maxMsgSize)
	return &client{conn: conn, out: make(chan []byte, sendBuffer), done: make(chan struct{})}
}

// send queues msg without blocking; a client that cannot keep up is dropped.
func (c *client) send(msg serverMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("client: marshal %s: %v", msg.Type, err)
		return
	}
	select {
	case c.out <- data:
	case <-c.done:
	default:
		c.close()
	}
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.CloseNow()
	})
}

// replaced tells the client why it is being disconnected. The close
// handshake can wait for the peer, so it runs on its own goroutine.
func (c *client) replaced() {
	c.once.Do(func() {
		close(c.done)
		go c.conn.Close(CloseReplaced, "replaced by another connection")
	})
}

func (c *client) writeLoop(ctx context.Context) {
	for {
		select {
		case data := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// readLoop passes the player's messages to the table until the connection ends.
func (c *client) readLoop(ctx context.Context, t *table, token string) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			logUnexpectedClose("client: read", err)
			return
		}
		var msg clientMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			c.send(serverMsg{Type: "error", Error: "bad message"})
			continue
		}
		if err := handleClientMsg(t, token, msg); err != nil {
			c.send(serverMsg{Type: "error", Error: err.Error()})
		}
	}
}

type userError string

func (e userError) Error() string { return string(e) }

func handleClientMsg(t *table, token string, msg clientMsg) error {
	switch msg.Type {
	case "act":
		a, ok := actionsByName[msg.Action]
		if !ok {
			return userError("unknown action " + msg.Action)
		}
		return t.act(token, a, msg.Amount)
	case "chat":
		text := strings.TrimSpace(msg.Text)
		if text == "" || utf8.RuneCountInString(text) > maxChatLen {
			return userError("chat message must be 1 to 200 characters")
		}
		t.chat(token, text)
		return nil
	}
	return userError("unknown message type " + msg.Type)
}
