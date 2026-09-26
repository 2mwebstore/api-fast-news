// Package websocket implements the breaking-news fan-out from §7:
// admin publishes -> API -> MySQL -> Redis -> WebSocket -> connected readers.
package websocket

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// EventType names the messages the frontend knows how to handle.
type EventType string

const (
	EventBreaking      EventType = "breaking"       // a new or updated breaking alert
	EventBreakingEnded EventType = "breaking_ended" // an alert was retired
	EventArticle       EventType = "article"        // a story was published
	EventTrending      EventType = "trending"       // the trending list was recomputed
	EventTraffic       EventType = "traffic"        // the traffic board changed
	EventPing          EventType = "ping"
)

// Event is the envelope sent to every subscriber.
type Event struct {
	Type      EventType `json:"type"`
	Payload   any       `json:"payload,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Hub tracks connected clients and broadcasts events to them.
//
// Broadcast never blocks: a client whose send buffer is full is dropped rather
// than allowed to stall the publish path. A reader losing a push is recoverable
// (the next poll or reconnect catches up); a stalled newsroom is not.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}

	register   chan *Client
	unregister chan *Client
	broadcast  chan Event
	done       chan struct{}

	// lastBreaking is replayed to each new connection so a reader who arrives
	// mid-event sees the current alert without waiting for the next one.
	lastBreaking *Event
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		broadcast:  make(chan Event, 256),
		done:       make(chan struct{}),
	}
}

// Run owns all mutations of the client set. Start it once, in a goroutine.
func (h *Hub) Run() {
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			count := len(h.clients)
			replay := h.lastBreaking
			h.mu.Unlock()
			slog.Info("websocket client connected", "clients", count)
			if replay != nil {
				client.trySend(*replay)
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			count := len(h.clients)
			h.mu.Unlock()
			slog.Info("websocket client disconnected", "clients", count)

		case event := <-h.broadcast:
			h.fanOut(event)

		case <-ping.C:
			h.fanOut(Event{Type: EventPing, Timestamp: time.Now().UTC()})

		case <-h.done:
			h.mu.Lock()
			for client := range h.clients {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			return
		}
	}
}

func (h *Hub) fanOut(event Event) {
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		targets = append(targets, client)
	}
	h.mu.RUnlock()

	var dropped int
	for _, client := range targets {
		if !client.trySend(event) {
			dropped++
			// Unregister asynchronously — this runs on the hub goroutine, so
			// sending to h.unregister synchronously would deadlock.
			select {
			case h.unregister <- client:
			default:
			}
		}
	}
	if dropped > 0 {
		slog.Warn("dropped slow websocket clients", "count", dropped, "event", event.Type)
	}
}

// Publish queues an event. It never blocks the caller: if the broadcast buffer
// is full the event is dropped with a warning, because a publish request must
// not hang on the push layer.
func (h *Hub) Publish(eventType EventType, payload any) {
	event := Event{Type: eventType, Payload: payload, Timestamp: time.Now().UTC()}

	if eventType == EventBreaking {
		h.mu.Lock()
		h.lastBreaking = &event
		h.mu.Unlock()
	}
	if eventType == EventBreakingEnded {
		h.mu.Lock()
		h.lastBreaking = nil
		h.mu.Unlock()
	}

	select {
	case h.broadcast <- event:
	default:
		slog.Warn("websocket broadcast buffer full, event dropped", "type", eventType)
	}
}

// Count returns the number of connected clients, for the admin dashboard.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close shuts the hub down.
func (h *Hub) Close() { close(h.done) }

// encode is used by Client; kept here so the wire format has one owner.
func encode(e Event) ([]byte, error) { return json.Marshal(e) }
