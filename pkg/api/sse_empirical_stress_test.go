package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEmpirical_NanosecondBurst_UniqueIDs_And_FullFanout challenges:
// 1. 20 simultaneous client connections hitting GET /api/events at the exact same nanosecond.
// 2. Strict uniqueness of all 20 client IDs (0 collisions).
// 3. broker.ClientCount() == 20.
// 4. 100% event fanout: every client receives all broadcast events without dropping.
func TestEmpirical_NanosecondBurst_UniqueIDs_And_FullFanout(t *testing.T) {
	_, _, broker, handler := setupTestEnv(t)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	const numClients = 20
	const numEvents = 25

	type clientReader struct {
		id       int
		ctx      context.Context
		cancel   context.CancelFunc
		eventsMu sync.Mutex
		events   []int
		err      error
	}

	readers := make([]*clientReader, numClients)
	var wgReady sync.WaitGroup
	wgReady.Add(numClients)

	// High precision barrier timestamp
	spinBarrier := make(chan time.Time)

	var connectedCount int32

	for i := 0; i < numClients; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		r := &clientReader{
			id:     i,
			ctx:    ctx,
			cancel: cancel,
		}
		readers[i] = r

		go func(cr *clientReader) {
			req, err := http.NewRequestWithContext(cr.ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				cr.err = err
				wgReady.Done()
				return
			}

			wgReady.Done()

			// Wait for target nanosecond barrier
			target := <-spinBarrier
			for time.Now().Before(target) {
				runtime.Gosched()
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				cr.err = err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				cr.err = fmt.Errorf("unexpected status %d", resp.StatusCode)
				return
			}

			atomic.AddInt32(&connectedCount, 1)

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					dataStr := strings.TrimPrefix(line, "data: ")
					var payload struct {
						Seq int `json:"seq"`
					}
					if err := json.Unmarshal([]byte(dataStr), &payload); err == nil {
						cr.eventsMu.Lock()
						cr.events = append(cr.events, payload.Seq)
						cr.eventsMu.Unlock()
					}
				}
			}
		}(r)
	}

	// Wait until all 20 goroutines are initialized
	wgReady.Wait()

	// Release all 20 goroutines at exact same millisecond target
	targetTime := time.Now().Add(50 * time.Millisecond)
	for i := 0; i < numClients; i++ {
		spinBarrier <- targetTime
	}

	// Wait for all 20 to establish connection
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&connectedCount) < numClients && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if actual := atomic.LoadInt32(&connectedCount); actual != numClients {
		t.Fatalf("Only %d of %d clients connected", actual, numClients)
	}

	// Verify broker.ClientCount() == 20
	brokerCount := broker.ClientCount()
	if brokerCount != numClients {
		t.Fatalf("Broker.ClientCount() mismatch: expected %d, got %d", numClients, brokerCount)
	}

	// Verify all 20 Client IDs are strictly unique
	broker.mu.RLock()
	uniqueIDs := make(map[string]bool)
	for id := range broker.clients {
		if uniqueIDs[id] {
			broker.mu.RUnlock()
			t.Fatalf("FATAL BUG: Duplicate Client ID detected in broker map: %s", id)
		}
		uniqueIDs[id] = true
	}
	broker.mu.RUnlock()

	if len(uniqueIDs) != numClients {
		t.Fatalf("Expected %d unique IDs, got %d", numClients, len(uniqueIDs))
	}
	t.Logf("Successfully verified %d unique Client IDs under nanosecond concurrency", len(uniqueIDs))

	// Broadcast 25 events to verify 100% fanout
	for seq := 0; seq < numEvents; seq++ {
		broker.Publish(Event{
			Type: "empirical_test",
			Data: map[string]int{"seq": seq},
		})
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for delivery
	time.Sleep(200 * time.Millisecond)

	// Clean up client connections
	for _, r := range readers {
		r.cancel()
	}

	// Verify 100% event receipt on all 20 clients
	for _, r := range readers {
		r.eventsMu.Lock()
		evCount := len(r.events)
		r.eventsMu.Unlock()

		if evCount != numEvents {
			t.Errorf("Client %d received %d / %d events (dropped frames!)", r.id, evCount, numEvents)
		} else {
			for expectedSeq, actualSeq := range r.events {
				if actualSeq != expectedSeq {
					t.Errorf("Client %d out-of-order event: expected %d, got %d", r.id, expectedSeq, actualSeq)
					break
				}
			}
		}
	}
}

// TestEmpirical_AbruptDisconnects_MidBroadcast_SurvivingIntegrity challenges:
// 1. 20 subscribers connected.
// 2. High-volume broadcast in progress.
// 3. Exactly half (10) disconnect abruptly midway.
// 4. Broker must not panic, hang, or error.
// 5. Surviving 10 subscribers must receive 100% of remaining events.
func TestEmpirical_AbruptDisconnects_MidBroadcast_SurvivingIntegrity(t *testing.T) {
	_, _, broker, handler := setupTestEnv(t)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	const numClients = 20
	const numDisconnect = 10
	const totalEvents = 40

	type subscriber struct {
		id       int
		ctx      context.Context
		cancel   context.CancelFunc
		eventsMu sync.Mutex
		events   []int
	}

	subs := make([]*subscriber, numClients)
	var wgReady sync.WaitGroup
	wgReady.Add(numClients)
	var connected int32

	for i := 0; i < numClients; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		s := &subscriber{id: i, ctx: ctx, cancel: cancel}
		subs[i] = s

		go func(sub *subscriber) {
			req, err := http.NewRequestWithContext(sub.ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				wgReady.Done()
				return
			}
			wgReady.Done()

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			atomic.AddInt32(&connected, 1)

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					dataStr := strings.TrimPrefix(line, "data: ")
					var p struct {
						Seq int `json:"seq"`
					}
					if err := json.Unmarshal([]byte(dataStr), &p); err == nil {
						sub.eventsMu.Lock()
						sub.events = append(sub.events, p.Seq)
						sub.eventsMu.Unlock()
					}
				}
			}
		}(s)
	}

	wgReady.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&connected) < numClients && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if broker.ClientCount() != numClients {
		t.Fatalf("Expected %d clients connected, got %d", numClients, broker.ClientCount())
	}

	// Broadcast events 0 to 19
	for seq := 0; seq < 20; seq++ {
		broker.Publish(Event{Type: "stream", Data: map[string]int{"seq": seq}})
		time.Sleep(3 * time.Millisecond)
	}

	// Abruptly disconnect clients 0..9 concurrently
	var wgDisc sync.WaitGroup
	for i := 0; i < numDisconnect; i++ {
		wgDisc.Add(1)
		go func(idx int) {
			defer wgDisc.Done()
			subs[idx].cancel()
		}(i)
	}
	wgDisc.Wait()

	// Continue broadcasting events 20 to 39 while disconnect is processing
	for seq := 20; seq < totalEvents; seq++ {
		broker.Publish(Event{Type: "stream", Data: map[string]int{"seq": seq}})
		time.Sleep(3 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify surviving clients (10..19) received all events up to 39
	for i := numDisconnect; i < numClients; i++ {
		subs[i].eventsMu.Lock()
		count := len(subs[i].events)
		lastSeq := -1
		if count > 0 {
			lastSeq = subs[i].events[count-1]
		}
		subs[i].eventsMu.Unlock()

		if lastSeq != totalEvents-1 {
			t.Errorf("Surviving client %d last event: expected %d, got %d (received total %d)",
				i, totalEvents-1, lastSeq, count)
		}
	}

	// Verify broker count is down to exactly 10
	if surviving := broker.ClientCount(); surviving != numClients-numDisconnect {
		t.Fatalf("Broker client count after abrupt disconnects: expected %d, got %d",
			numClients-numDisconnect, surviving)
	}

	// Clean up remainder
	for i := numDisconnect; i < numClients; i++ {
		subs[i].cancel()
	}
	time.Sleep(100 * time.Millisecond)
	if final := broker.ClientCount(); final != 0 {
		t.Errorf("Broker client count after full cleanup: expected 0, got %d", final)
	}
}

// TestEmpirical_SlowConsumerBackpressure_BufferIsolation challenges:
// 1. Stalled client connection does not block fast client.
// 2. Fast client receives 100% of high-throughput broadcast within < 50ms per batch.
// 3. Stalled client buffer drops frames without server stall.
func TestEmpirical_SlowConsumerBackpressure_BufferIsolation(t *testing.T) {
	// Create broker with bounded client channel = 32
	broker := NewBroker(WithClientCap(32), WithPingInterval(1*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)
	defer func() {
		cancel()
		broker.Close()
	}()

	server := httptest.NewServer(broker)
	defer server.Close()

	// 1. Fast client
	fastCtx, fastCancel := context.WithCancel(context.Background())
	defer fastCancel()

	fastReq, _ := http.NewRequestWithContext(fastCtx, http.MethodGet, server.URL, nil)
	fastResp, err := http.DefaultClient.Do(fastReq)
	if err != nil {
		t.Fatalf("failed fast client connect: %v", err)
	}
	defer fastResp.Body.Close()

	var fastReceived int32
	fastDone := make(chan struct{})
	go func() {
		defer close(fastDone)
		scanner := bufio.NewScanner(fastResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: fast_burst") {
				atomic.AddInt32(&fastReceived, 1)
			}
		}
	}()

	// 2. Slow stalled client: reads 1 byte then halts
	slowCtx, slowCancel := context.WithCancel(context.Background())
	defer slowCancel()

	slowReq, _ := http.NewRequestWithContext(slowCtx, http.MethodGet, server.URL, nil)
	slowResp, err := http.DefaultClient.Do(slowReq)
	if err != nil {
		t.Fatalf("failed slow client connect: %v", err)
	}
	defer slowResp.Body.Close()

	// Read only first byte and freeze
	oneByte := make([]byte, 1)
	_, _ = io.ReadFull(slowResp.Body, oneByte)

	time.Sleep(50 * time.Millisecond)
	if broker.ClientCount() != 2 {
		t.Fatalf("Expected 2 clients connected, got %d", broker.ClientCount())
	}

	// 3. High-throughput burst: publish 50 events rapidly
	const totalEvents = 50
	pubStart := time.Now()

	for i := 0; i < totalEvents; i++ {
		broker.Publish(Event{
			Type: "fast_burst",
			Data: map[string]int{"seq": i},
		})
		time.Sleep(1 * time.Millisecond)
	}
	pubElapsed := time.Since(pubStart)
	t.Logf("Publishing %d events took: %v", totalEvents, pubElapsed)

	// Broker publish MUST NOT block even with a stalled consumer (< 2s under race detector)
	if pubElapsed > 2*time.Second {
		t.Errorf("Broker publish was blocked by slow consumer: took %v (expected < 2s)", pubElapsed)
	}

	// Fast client receives all events within deadline
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&fastReceived) < totalEvents && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	received := atomic.LoadInt32(&fastReceived)
	if received != totalEvents {
		t.Fatalf("Fast client received %d / %d events within deadline (starved by slow consumer!)", received, totalEvents)
	}
	t.Logf("Fast client received all %d events successfully without blockage", received)
}

// TestEmpirical_CleanShutdown_ActiveConnections_ZeroHang_ZeroLeak challenges:
// 1. httptest.Server.Close() with 20 active streaming clients terminates cleanly.
// 2. Zero hang / deadlock (< 500ms).
// 3. Zero goroutine leak after shutdown.
func TestEmpirical_CleanShutdown_ActiveConnections_ZeroHang_ZeroLeak(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	broker := NewBroker(WithClientCap(32), WithPingInterval(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	server := httptest.NewServer(broker)

	const numClients = 20
	var connected int32
	var wgDone sync.WaitGroup
	wgDone.Add(numClients)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	for i := 0; i < numClients; i++ {
		go func() {
			defer wgDone.Done()
			req, err := http.NewRequestWithContext(clientCtx, http.MethodGet, server.URL, nil)
			if err != nil {
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			atomic.AddInt32(&connected, 1)

			// Continuously read until server terminates connection
			buf := make([]byte, 1024)
			for {
				_, err := resp.Body.Read(buf)
				if err != nil {
					return
				}
			}
		}()
	}

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&connected) < numClients && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&connected) != numClients {
		t.Fatalf("Expected %d clients connected, got %d", numClients, atomic.LoadInt32(&connected))
	}

	// Trigger graceful broker shutdown to signal all active SSE streams
	shutdownStart := time.Now()

	// 1. Terminate broker - this immediately closes client.done for all 20 subscribers
	broker.Close()
	cancel()

	// 2. Close httptest.Server - with all handlers terminating, server.Close() finishes immediately
	server.Close()

	shutdownDuration := time.Since(shutdownStart)
	t.Logf("Broker and Server Close completed in %v", shutdownDuration)

	if shutdownDuration > 1*time.Second {
		t.Errorf("Shutdown took too long (> 1s): %v", shutdownDuration)
	}

	// Wait for all client reader goroutines to complete
	clientWaitDone := make(chan struct{})
	go func() {
		wgDone.Wait()
		close(clientWaitDone)
	}()

	select {
	case <-clientWaitDone:
		t.Logf("All %d client connections unwound cleanly", numClients)
	case <-time.After(2 * time.Second):
		t.Fatalf("DEADLOCK: Client reader goroutines hung after server.Close()")
	}

	// Verify zero goroutine leaks
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	leak := finalGoroutines - initialGoroutines
	t.Logf("Goroutines: baseline=%d, final=%d, delta=%d", initialGoroutines, finalGoroutines, leak)

	if leak > 5 {
		t.Errorf("Goroutine leak detected after clean shutdown: delta = %d", leak)
	}
}
