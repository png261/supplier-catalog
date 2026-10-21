package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Event represents an SSE event frame or keepalive comment.
type Event struct {
	Type      string    `json:"type,omitempty"`
	JobID     string    `json:"job_id,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Data      any       `json:"data,omitempty"`
	Comment   string    `json:"-"` // Non-empty triggers comment frame: ": ping\n\n"
}

// Client represents an active HTTP SSE connection.
type Client struct {
	id       string
	messages chan Event
	done     chan struct{}
}

// BrokerOption configures a Broker instance.
type BrokerOption func(*Broker)

// WithPingInterval configures the keepalive heartbeat interval.
func WithPingInterval(d time.Duration) BrokerOption {
	return func(b *Broker) {
		b.pingInterval = d
	}
}

// WithClientCap configures the bounded channel capacity per subscriber.
func WithClientCap(c int) BrokerOption {
	return func(b *Broker) {
		b.clientCap = c
	}
}

// Broker coordinates event fan-out to connected clients with backpressure handling.
type Broker struct {
	mu           sync.RWMutex
	clients      map[string]*Client
	broadcast    chan Event
	register     chan *Client
	unregister   chan *Client
	quit         chan struct{}
	pingTicker   *time.Ticker
	pingInterval time.Duration
	clientCap    int
	nextClientID uint64
	closed       bool
}

// NewBroker initializes a thread-safe SSE broker.
func NewBroker(opts ...BrokerOption) *Broker {
	b := &Broker{
		clients:      make(map[string]*Client),
		broadcast:    make(chan Event, 256),
		register:     make(chan *Client, 128),
		unregister:   make(chan *Client, 128),
		quit:         make(chan struct{}),
		pingInterval: 15 * time.Second,
		clientCap:    64,
	}
	for _, opt := range opts {
		opt(b)
	}
	b.pingTicker = time.NewTicker(b.pingInterval)
	return b
}

// Start begins the central event dispatch loop.
func (b *Broker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			b.Close()
			return
		case <-b.quit:
			return
		case client := <-b.register:
			b.mu.Lock()
			if !b.closed {
				if client.id == "" {
					client.id = fmt.Sprintf("client-%d", atomic.AddUint64(&b.nextClientID, 1))
				}
				b.clients[client.id] = client
			}
			b.mu.Unlock()
		case client := <-b.unregister:
			b.mu.Lock()
			if c, ok := b.clients[client.id]; ok && c == client {
				delete(b.clients, client.id)
				select {
				case <-c.done:
				default:
					close(c.done)
				}
			}
			b.mu.Unlock()
		case evt := <-b.broadcast:
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.messages <- evt:
				default:
					// Backpressure: client buffer full -> drop frame to avoid blocking broker
				}
			}
			b.mu.RUnlock()
		case <-b.pingTicker.C:
			// Heartbeat comment frame
			ping := Event{Comment: "ping"}
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.messages <- ping:
				default:
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Publish broadcasts an event to all connected clients.
func (b *Broker) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return
	}

	select {
	case b.broadcast <- evt:
	case <-b.quit:
	}
}

// ClientCount returns the number of currently active subscribers.
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// Close gracefully terminates the broker and disconnects clients.
func (b *Broker) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.quit)
	b.pingTicker.Stop()

	for id, client := range b.clients {
		select {
		case <-client.done:
		default:
			close(client.done)
		}
		delete(b.clients, id)
	}
	b.mu.Unlock()
}

// ServeHTTP handles HTTP SSE streaming connections.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"Streaming unsupported","code":"STREAMING_UNSUPPORTED"}`, http.StatusInternalServerError)
		return
	}

	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		http.Error(w, `{"error":"Broker closed","code":"BROKER_CLOSED"}`, http.StatusServiceUnavailable)
		return
	}

	client := &Client{
		id:       fmt.Sprintf("client-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&b.nextClientID, 1)),
		messages: make(chan Event, b.clientCap),
		done:     make(chan struct{}),
	}

	select {
	case b.register <- client:
	case <-b.quit:
		return
	}

	defer func() {
		select {
		case b.unregister <- client:
		case <-b.quit:
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// W3C reconnect interval instruction: retry in 3000ms
	fmt.Fprintf(w, "retry: 3000\n\n")
	flusher.Flush()

	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return
		case <-b.quit:
			return
		case <-client.done:
			return
		case evt, ok := <-client.messages:
			if !ok {
				return
			}
			if evt.Comment != "" {
				fmt.Fprintf(w, ": %s\n\n", evt.Comment)
			} else {
				dataBytes, err := json.Marshal(evt.Data)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, string(dataBytes))
			}
			flusher.Flush()
		}
	}
}
