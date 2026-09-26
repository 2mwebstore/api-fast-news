package websocket

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 70 * time.Second
	pingPeriod     = 50 * time.Second
	maxMessageSize = 1024 // readers never send anything substantial
	sendBuffer     = 32
)

// Client is one browser connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan Event
}

// trySend queues an event without blocking. It returns false when the client's
// buffer is full, which the hub treats as "this client is too slow, drop it".
func (c *Client) trySend(e Event) bool {
	select {
	case c.send <- e:
		return true
	default:
		return false
	}
}

// Upgrader builds the HTTP->WebSocket upgrader. Origins are checked against the
// configured allowlist: without this, any site could open a socket to the API.
func Upgrader(allowedOrigins []string) websocket.Upgrader {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimRight(r.Header.Get("Origin"), "/")
			if origin == "" {
				// Native clients and curl send no Origin; browsers always do.
				return true
			}
			return allowed[origin]
		},
	}
}

// Serve upgrades the connection and starts its read and write pumps.
func Serve(hub *Hub, up websocket.Upgrader, w http.ResponseWriter, r *http.Request) {
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "error", err)
		return
	}

	client := &Client{hub: hub, conn: conn, send: make(chan Event, sendBuffer)}
	hub.register <- client

	go client.writePump()
	go client.readPump()
}

// readPump exists to observe pongs and detect a dead peer. Inbound messages
// are discarded — this is a one-way broadcast channel.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("websocket closed unexpectedly", "error", err)
			}
			return
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			payload, err := encode(event)
			if err != nil {
				slog.Warn("websocket encode failed", "type", event.Type, "error", err)
				continue
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
