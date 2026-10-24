package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/echo", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func TestEchoReturnsMessages(t *testing.T) {
	srv := httptest.NewServer(New(Options{}))
	defer srv.Close()
	c := dial(t, srv)

	tests := []struct {
		typ  websocket.MessageType
		data string
	}{
		{websocket.MessageText, "привет"},
		{websocket.MessageText, `{"type":"act","action":"raise","amount":200}`},
		{websocket.MessageBinary, "\x00\x01\x02"},
	}
	for _, tt := range tests {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := c.Write(ctx, tt.typ, []byte(tt.data)); err != nil {
			cancel()
			t.Fatalf("write %q: %v", tt.data, err)
		}
		typ, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ != tt.typ || string(data) != tt.data {
			t.Errorf("echo = %v %q, want %v %q", typ, data, tt.typ, tt.data)
		}
	}
	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestEchoServesClientsSeparately(t *testing.T) {
	srv := httptest.NewServer(New(Options{}))
	defer srv.Close()
	a, b := dial(t, srv), dial(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Write(ctx, websocket.MessageText, []byte("from a")); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := b.Write(ctx, websocket.MessageText, []byte("from b")); err != nil {
		t.Fatalf("write b: %v", err)
	}
	for name, c := range map[string]*websocket.Conn{"from a": a, "from b": b} {
		_, data, err := c.Read(ctx)
		if err != nil || string(data) != name {
			t.Errorf("client got %q, %v; want %q", data, err, name)
		}
	}
}

func TestPlainHTTPToWebSocketIsRejected(t *testing.T) {
	srv := httptest.NewServer(New(Options{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/echo")
	if err != nil {
		t.Fatalf("GET /echo: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUpgradeRequired)
	}
}

func TestIndex(t *testing.T) {
	srv := httptest.NewServer(New(Options{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
