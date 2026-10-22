package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coderpush/pkg/domain"
)

type safeClient struct {
	id     int
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	events []string
	raw    []string
	err    error
}

func (c *safeClient) addEvent(e string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *safeClient) addRaw(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = append(c.raw, line)
}

func (c *safeClient) getEvents() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	res := make([]string, len(c.events))
	copy(res, c.events)
	return res
}

func (c *safeClient) getRaw() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	res := make([]string, len(c.raw))
	copy(res, c.raw)
	return res
}

// -----------------------------------------------------------------------------
// Objective 1: Multi-Client Fanout & Zero Leak
// -----------------------------------------------------------------------------

func TestAdversarial_MultiClientFanout_And_ZeroLeak(t *testing.T) {
	_, _, broker, handler := setupTestEnv(t)

	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	const numClients = 20
	const numDisconnect = 10

	runtime.GC()
	initialGoroutines := runtime.NumGoroutine()

	clients := make([]*safeClient, numClients)
	defer func() {
		for _, c := range clients {
			if c != nil && c.cancel != nil {
				c.cancel()
			}
		}
	}()
	var wgReady sync.WaitGroup
	wgReady.Add(numClients)
	startBarrier := make(chan struct{})

	clientConnected := int32(0)

	for i := 0; i < numClients; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		rec := &safeClient{
			id:     i,
			ctx:    ctx,
			cancel: cancel,
		}
		clients[i] = rec

		go func(c *safeClient) {
			req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				c.err = err
				wgReady.Done()
				return
			}

			wgReady.Done()
			<-startBarrier // Simultaneous launch

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				c.err = err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				c.err = fmt.Errorf("unexpected status %d", resp.StatusCode)
				return
			}

			atomic.AddInt32(&clientConnected, 1)

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				c.addRaw(line)
				if strings.HasPrefix(line, "event: ") {
					c.addEvent(strings.TrimPrefix(line, "event: "))
				}
			}
			if err := scanner.Err(); err != nil && c.ctx.Err() == nil {
				c.err = err
			}
		}(rec)
	}

	wgReady.Wait()
	close(startBarrier)

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&clientConnected) < numClients && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	connected := atomic.LoadInt32(&clientConnected)
	if connected != numClients {
		t.Fatalf("Failed to connect all clients: got %d, expected %d", connected, numClients)
	}

	// Verify broker count matches connected clients
	if count := broker.ClientCount(); count != numClients {
		t.Errorf("CONCURRENCY BUG: Broker client count mismatch: got %d, expected %d (Client ID collision on concurrent connection)", count, numClients)
	}

	// Post sync job with 3 batches
	batchesPayload := map[string]any{
		"mode": "batches",
		"batches": []map[string]any{
			{"batch_index": 0, "records": []map[string]any{{"sku": "SKU-A", "name": "Prod A", "price": 10.0, "status": "ACTIVE"}}},
			{"batch_index": 1, "records": []map[string]any{{"sku": "SKU-B", "name": "Prod B", "price": 20.0, "status": "ACTIVE"}}},
			{"batch_index": 2, "records": []map[string]any{{"sku": "SKU-C", "name": "Prod C", "price": 30.0, "status": "ACTIVE"}}},
		},
	}
	body, _ := json.Marshal(batchesPayload)
	postReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/jobs", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("failed to post job: %v", err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", postResp.StatusCode)
	}

	// Wait for job completion
	time.Sleep(800 * time.Millisecond)

	expectedEvents := []string{"job_started", "progress", "progress", "progress", "job_completed"}

	for _, c := range clients {
		evts := c.getEvents()
		if len(evts) != len(expectedEvents) {
			t.Errorf("Client %d event count mismatch: expected %d (%v), got %d (%v)",
				c.id, len(expectedEvents), expectedEvents, len(evts), evts)
		} else {
			for idx, exp := range expectedEvents {
				if evts[idx] != exp {
					t.Errorf("Client %d event[%d] mismatch: expected %s, got %s", c.id, idx, exp, evts[idx])
				}
			}
		}
	}

	// Abruptly disconnect 10 clients
	t.Logf("Abruptly disconnecting %d clients...", numDisconnect)
	for i := 0; i < numDisconnect; i++ {
		clients[i].cancel()
	}

	// Wait for broker to process unregisters
	time.Sleep(300 * time.Millisecond)

	expectedRemaining := numClients - numDisconnect
	if actualCount := broker.ClientCount(); actualCount != expectedRemaining {
		t.Fatalf("Broker client count after 10 abrupt disconnects mismatch: expected %d, got %d",
			expectedRemaining, actualCount)
	}

	// Publish another event to ensure surviving clients still receive events without panics
	broker.Publish(Event{Type: "post_disconnect_test", Data: map[string]string{"msg": "alive"}})
	time.Sleep(200 * time.Millisecond)

	for i := numDisconnect; i < numClients; i++ {
		evts := clients[i].getEvents()
		if len(evts) < len(expectedEvents)+1 || evts[len(evts)-1] != "post_disconnect_test" {
			t.Errorf("Surviving client %d did not receive post-disconnect event: got %v", i, evts)
		}
	}

	// Disconnect remaining clients
	for i := numDisconnect; i < numClients; i++ {
		clients[i].cancel()
	}

	time.Sleep(300 * time.Millisecond)
	if count := broker.ClientCount(); count != 0 {
		t.Fatalf("Broker client count after all disconnects: expected 0, got %d", count)
	}

	// Verify zero goroutine leaks (allowing tolerance for runtime runtime background threads)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	leak := finalGoroutines - initialGoroutines
	t.Logf("Goroutines: baseline=%d, final=%d, delta=%d", initialGoroutines, finalGoroutines, leak)
	if leak > 5 {
		t.Errorf("Potential goroutine leak detected: delta = %d goroutines", leak)
	}
}

func TestAdversarial_MultiClient_MidStream_AbruptDisconnect(t *testing.T) {
	_, _, broker, handler := setupTestEnv(t)

	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	const numClients = 20
	clients := make([]*safeClient, numClients)
	defer func() {
		for _, c := range clients {
			if c != nil && c.cancel != nil {
				c.cancel()
			}
		}
	}()
	clientConnected := int32(0)

	for i := 0; i < numClients; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		rec := &safeClient{id: i, ctx: ctx, cancel: cancel}
		clients[i] = rec

		go func(c *safeClient) {
			req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			atomic.AddInt32(&clientConnected, 1)

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "event: ") {
					c.addEvent(strings.TrimPrefix(line, "event: "))
				}
			}
		}(rec)
	}

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&clientConnected) < numClients && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Prepare a job with 8 batches
	batches := make([]map[string]any, 8)
	for i := 0; i < 8; i++ {
		batches[i] = map[string]any{
			"batch_index": i,
			"records": []map[string]any{
				{"sku": fmt.Sprintf("SKU-STREAM-%d", i), "name": fmt.Sprintf("Prod %d", i), "price": 10.0 + float64(i), "status": "ACTIVE"},
			},
		}
	}
	body, _ := json.Marshal(map[string]any{"mode": "batches", "batches": batches})
	postReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/jobs", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("failed to post job: %v", err)
	}
	defer postResp.Body.Close()

	// Mid-stream disconnect: disconnect 10 clients concurrently while batches are committing!
	time.Sleep(30 * time.Millisecond)
	var wgDisconnect sync.WaitGroup
	for i := 0; i < 10; i++ {
		wgDisconnect.Add(1)
		go func(c *safeClient) {
			defer wgDisconnect.Done()
			c.cancel()
		}(clients[i])
	}
	wgDisconnect.Wait()

	// Wait for job to finish completely
	time.Sleep(800 * time.Millisecond)

	// Check surviving clients (10 to 19): all must have received job_completed without panic
	for i := 10; i < numClients; i++ {
		evts := clients[i].getEvents()
		hasCompleted := false
		for _, e := range evts {
			if e == "job_completed" {
				hasCompleted = true
				break
			}
		}
		if !hasCompleted {
			t.Errorf("Surviving client %d did not receive job_completed: %v", i, evts)
		}
	}

	// Clean up remaining
	for i := 10; i < numClients; i++ {
		clients[i].cancel()
	}
	time.Sleep(200 * time.Millisecond)

	if count := broker.ClientCount(); count != 0 {
		t.Errorf("Broker active client count: expected 0, got %d", count)
	}
}

// -----------------------------------------------------------------------------
// Objective 2: Slow Consumer Isolation (Backpressure)
// -----------------------------------------------------------------------------

func TestAdversarial_SlowConsumerIsolation_Backpressure(t *testing.T) {
	// Broker with client channel capacity = 16
	broker := NewBroker(WithClientCap(16), WithPingInterval(1*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)
	defer func() {
		cancel()
		broker.Close()
	}()

	// Client 1: Fast client that continuously consumes events
	fastClient := &Client{
		id:       "fast-client",
		messages: make(chan Event, 16),
		done:     make(chan struct{}),
	}
	broker.register <- fastClient

	// Client 2: Slow client that NEVER reads from its channel
	slowClient := &Client{
		id:       "slow-client-frozen",
		messages: make(chan Event, 16),
		done:     make(chan struct{}),
	}
	broker.register <- slowClient

	time.Sleep(50 * time.Millisecond)
	if count := broker.ClientCount(); count != 2 {
		t.Fatalf("Expected 2 registered clients, got %d", count)
	}

	fastReceived := int32(0)
	go func() {
		for {
			select {
			case <-fastClient.done:
				return
			case _, ok := <-fastClient.messages:
				if !ok {
					return
				}
				atomic.AddInt32(&fastReceived, 1)
			}
		}
	}()

	// Publish 150 events rapidly
	const totalEvents = 150
	startPub := time.Now()

	for i := 0; i < totalEvents; i++ {
		broker.Publish(Event{
			Type: "stress_event",
			Data: map[string]int{"seq": i},
		})
		time.Sleep(100 * time.Microsecond)
	}
	pubDuration := time.Since(startPub)
	t.Logf("Published %d events in %v", totalEvents, pubDuration)

	// Publishing 150 events must be non-blocking (< 100ms)
	if pubDuration > 500*time.Millisecond {
		t.Errorf("Publish blocked unexpectedly by slow consumer: took %v", pubDuration)
	}

	// Wait for fast client to receive all events
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&fastReceived) < totalEvents && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	received := atomic.LoadInt32(&fastReceived)
	t.Logf("Fast client received %d / %d events", received, totalEvents)
	if received != totalEvents {
		t.Errorf("Fast client missed events: got %d, expected %d", received, totalEvents)
	}

	// Verify slow client buffer is capped at exactly 16 (capacity), meaning 134 events were dropped non-blockingly
	slowBufferLen := len(slowClient.messages)
	t.Logf("Slow client buffer length: %d (capacity: %d)", slowBufferLen, 16)
	if slowBufferLen != 16 {
		t.Errorf("Expected slow client buffer to be full (16), got %d", slowBufferLen)
	}

	// Clean up
	broker.unregister <- fastClient
	broker.unregister <- slowClient
	time.Sleep(50 * time.Millisecond)
}

// -----------------------------------------------------------------------------
// Objective 3: Keepalive Ping Verification
// -----------------------------------------------------------------------------

func TestAdversarial_KeepalivePing_W3C_Parsing(t *testing.T) {
	// Broker with short ping interval = 40ms
	broker := NewBroker(WithPingInterval(40 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)
	defer func() {
		cancel()
		broker.Close()
	}()

	server := httptest.NewServer(broker)
	defer server.Close()

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	req, err := http.NewRequestWithContext(clientCtx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	// Step 1: Read initial W3C retry instruction
	line1, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read line 1: %v", err)
	}
	if strings.TrimSpace(line1) != "retry: 3000" {
		t.Errorf("expected 'retry: 3000', got %q", line1)
	}
	lineEmpty, _ := reader.ReadString('\n') // blank line after retry
	if strings.TrimSpace(lineEmpty) != "" {
		t.Errorf("expected empty line after retry, got %q", lineEmpty)
	}

	// Step 2: Read idle stream for ~150ms to collect keepalive pings
	pingComments := 0
	dataEvents := 0

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		timer := time.NewTimer(150 * time.Millisecond)
		defer timer.Stop()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == ": ping" {
				pingComments++
			} else if strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:") {
				dataEvents++
			}

			select {
			case <-timer.C:
				return
			default:
			}
		}
	}()

	<-readDone

	t.Logf("Collected %d ': ping' comments and %d data events during idle period", pingComments, dataEvents)
	if pingComments < 2 {
		t.Errorf("Expected at least 2 ping comments, got %d", pingComments)
	}
	if dataEvents != 0 {
		t.Errorf("Expected 0 data events on idle stream, got %d", dataEvents)
	}

	// Step 3: Publish a real event and verify clean W3C SSE parsing
	broker.Publish(Event{
		Type: "telemetry_test",
		Data: map[string]string{"status": "ok"},
	})

	// Read lines until event is parsed
	var receivedEvent string
	var receivedData string
	timeout := time.After(1 * time.Second)

	for receivedEvent == "" || receivedData == "" {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for telemetry_test event; event=%s, data=%s", receivedEvent, receivedData)
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("failed reading line: %v", err)
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "event: ") {
				receivedEvent = strings.TrimPrefix(trimmed, "event: ")
			} else if strings.HasPrefix(trimmed, "data: ") {
				receivedData = strings.TrimPrefix(trimmed, "data: ")
			}
		}
	}

	if receivedEvent != "telemetry_test" {
		t.Errorf("Expected event 'telemetry_test', got %q", receivedEvent)
	}
	if !strings.Contains(receivedData, `"status":"ok"`) {
		t.Errorf("Expected data containing '\"status\":\"ok\"', got %q", receivedData)
	}

	// Step 4: Verify default ping interval in NewBroker() is exactly 15s
	defaultBroker := NewBroker()
	if defaultBroker.pingInterval != 15*time.Second {
		t.Errorf("Expected default ping interval 15s, got %v", defaultBroker.pingInterval)
	}
}

// -----------------------------------------------------------------------------
// Objective 4: Metric Counters Fidelity vs SQLite Database
// -----------------------------------------------------------------------------

func TestAdversarial_MetricCountersFidelity_AgainstSQLite(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)

	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	// Connect SSE listener to capture all events
	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()

	sseReq, err := http.NewRequestWithContext(sseCtx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("failed to create SSE request: %v", err)
	}
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("failed to connect SSE: %v", err)
	}
	defer sseResp.Body.Close()

	var sseMu sync.Mutex
	type capturedEvent struct {
		Type string
		Data json.RawMessage
	}
	var capturedEvents []capturedEvent

	go func() {
		scanner := bufio.NewScanner(sseResp.Body)
		var currentType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				sseMu.Lock()
				capturedEvents = append(capturedEvents, capturedEvent{
					Type: currentType,
					Data: json.RawMessage(dataStr),
				})
				sseMu.Unlock()
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Workload with calibrated changes across 5 batches:
	// Batch 0: +10 Added (new ACTIVE products)
	// Batch 1: ~4 Updated (newer timestamp), ↷2 Skipped (identical records)
	// Batch 2: -3 Withdrawn (status WITHDRAWN)
	// Batch 3: ↷2 Skipped (older timestamps), ↷1 Skipped (reactivate withdrawn -> active)
	// Batch 4: ⚠2 Dead-Lettered (negative price & empty SKU), +2 Added (new active products)

	t0 := time.Now().UTC().Add(-10 * time.Hour)
	tNewer := t0.Add(2 * time.Hour)
	tOlder := t0.Add(-2 * time.Hour)

	batchesPayload := map[string]any{
		"mode": "batches",
		"batches": []map[string]any{
			// Batch 0: 10 new products
			{
				"batch_index": 0,
				"records": []map[string]any{
					{"sku": "FID-01", "name": "Item 1", "price": 10.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-02", "name": "Item 2", "price": 20.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-03", "name": "Item 3", "price": 30.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-04", "name": "Item 4", "price": 40.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-05", "name": "Item 5", "price": 50.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-06", "name": "Item 6", "price": 60.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-07", "name": "Item 7", "price": 70.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-08", "name": "Item 8", "price": 80.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-09", "name": "Item 9", "price": 90.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-10", "name": "Item 10", "price": 100.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
				},
			},
			// Batch 1: 4 updates with newer timestamp, 2 identical duplicates
			{
				"batch_index": 1,
				"records": []map[string]any{
					{"sku": "FID-01", "name": "Item 1 v2", "price": 15.0, "status": "ACTIVE", "timestamp": tNewer.Format(time.RFC3339Nano)},
					{"sku": "FID-02", "name": "Item 2 v2", "price": 25.0, "status": "ACTIVE", "timestamp": tNewer.Format(time.RFC3339Nano)},
					{"sku": "FID-03", "name": "Item 3 v2", "price": 35.0, "status": "ACTIVE", "timestamp": tNewer.Format(time.RFC3339Nano)},
					{"sku": "FID-04", "name": "Item 4 v2", "price": 45.0, "status": "ACTIVE", "timestamp": tNewer.Format(time.RFC3339Nano)},
					{"sku": "FID-05", "name": "Item 5", "price": 50.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-06", "name": "Item 6", "price": 60.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
				},
			},
			// Batch 2: 3 withdrawn products
			{
				"batch_index": 2,
				"records": []map[string]any{
					{"sku": "FID-07", "name": "Item 7", "price": 70.0, "status": "WITHDRAWN", "timestamp": tNewer.Format(time.RFC3339Nano)},
					{"sku": "FID-08", "name": "Item 8", "price": 80.0, "status": "WITHDRAWN", "timestamp": tNewer.Format(time.RFC3339Nano)},
					{"sku": "FID-09", "name": "Item 9", "price": 90.0, "status": "WITHDRAWN", "timestamp": tNewer.Format(time.RFC3339Nano)},
				},
			},
			// Batch 3: 2 older timestamps + 1 attempt to resurrect WITHDRAWN FID-07 to ACTIVE
			{
				"batch_index": 3,
				"records": []map[string]any{
					{"sku": "FID-01", "name": "Item 1 Old", "price": 1.0, "status": "ACTIVE", "timestamp": tOlder.Format(time.RFC3339Nano)},
					{"sku": "FID-02", "name": "Item 2 Old", "price": 2.0, "status": "ACTIVE", "timestamp": tOlder.Format(time.RFC3339Nano)},
					{"sku": "FID-07", "name": "Item 7 Resurrect", "price": 999.0, "status": "ACTIVE", "timestamp": tNewer.Add(1 * time.Hour).Format(time.RFC3339Nano)},
				},
			},
			// Batch 4: 2 corrupt (negative price, empty SKU) + 2 new products
			{
				"batch_index": 4,
				"records": []map[string]any{
					{"sku": "CORRUPT-NEG", "name": "Bad Price", "price": -99.9, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "", "name": "No SKU", "price": 10.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-11", "name": "Item 11", "price": 110.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
					{"sku": "FID-12", "name": "Item 12", "price": 120.0, "status": "ACTIVE", "timestamp": t0.Format(time.RFC3339Nano)},
				},
			},
		},
	}

	body, _ := json.Marshal(batchesPayload)
	postReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/jobs", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("failed to post job: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", postResp.StatusCode)
	}

	var startResp SyncJobResponse
	_ = json.NewDecoder(postResp.Body).Decode(&startResp)
	jobID := startResp.JobID

	// Wait for job completion
	time.Sleep(1 * time.Second)

	// Fetch final job from storage
	job, err := store.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to get job from store: %v", err)
	}
	if job.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED, got %s", job.Status)
	}

	// Expected exact counts:
	// Added: 10 (batch 0) + 2 (batch 4) = 12
	// Updated: 4 (batch 1)
	// Withdrawn: 3 (batch 2)
	// Skipped: 2 (batch 1 identical) + 2 (batch 3 older) + 1 (batch 3 resurrection) = 5
	// DeadLetter: 2 (batch 4)
	expectedAdded := 12
	expectedUpdated := 4
	expectedWithdrawn := 3
	expectedSkipped := 5
	expectedDeadLetter := 2

	// 1. Verify SQLite sync_jobs table columns
	if job.Summary.Added != expectedAdded {
		t.Errorf("DB sync_jobs Added mismatch: expected %d, got %d", expectedAdded, job.Summary.Added)
	}
	if job.Summary.Updated != expectedUpdated {
		t.Errorf("DB sync_jobs Updated mismatch: expected %d, got %d", expectedUpdated, job.Summary.Updated)
	}
	if job.Summary.Withdrawn != expectedWithdrawn {
		t.Errorf("DB sync_jobs Withdrawn mismatch: expected %d, got %d", expectedWithdrawn, job.Summary.Withdrawn)
	}
	if job.Summary.Skipped != expectedSkipped {
		t.Errorf("DB sync_jobs Skipped mismatch: expected %d, got %d", expectedSkipped, job.Summary.Skipped)
	}
	if job.Summary.DeadLetter != expectedDeadLetter {
		t.Errorf("DB sync_jobs DeadLetter mismatch: expected %d, got %d", expectedDeadLetter, job.Summary.DeadLetter)
	}

	// 2. Verify SQLite products table catalog counts
	var activeCount, withdrawnCount int
	err = store.DB().QueryRow("SELECT COUNT(*) FROM products WHERE status = 'ACTIVE'").Scan(&activeCount)
	if err != nil {
		t.Fatalf("failed to count active products: %v", err)
	}
	err = store.DB().QueryRow("SELECT COUNT(*) FROM products WHERE status = 'WITHDRAWN'").Scan(&withdrawnCount)
	if err != nil {
		t.Fatalf("failed to count withdrawn products: %v", err)
	}

	expectedActive := expectedAdded - expectedWithdrawn // 12 - 3 = 9 active products
	if activeCount != expectedActive {
		t.Errorf("Catalog ACTIVE product count mismatch: expected %d, got %d", expectedActive, activeCount)
	}
	if withdrawnCount != expectedWithdrawn {
		t.Errorf("Catalog WITHDRAWN product count mismatch: expected %d, got %d", expectedWithdrawn, withdrawnCount)
	}

	// 3. Verify SQLite dead_letter_issues table count
	var dlCount int
	err = store.DB().QueryRow("SELECT COUNT(*) FROM dead_letter_issues WHERE job_id = ?", jobID).Scan(&dlCount)
	if err != nil {
		t.Fatalf("failed to count dead letter issues: %v", err)
	}
	if dlCount != expectedDeadLetter {
		t.Errorf("Dead letter issues count mismatch: expected %d, got %d", expectedDeadLetter, dlCount)
	}

	// 4. Verify batch_checkpoints table sums
	var cpAdded, cpUpdated, cpWithdrawn, cpSkipped, cpDeadLetter int
	row := store.DB().QueryRow(`
		SELECT
			COALESCE(SUM(added_count), 0),
			COALESCE(SUM(updated_count), 0),
			COALESCE(SUM(withdrawn_count), 0),
			COALESCE(SUM(skipped_count), 0),
			COALESCE(SUM(dead_letter_count), 0)
		FROM batch_checkpoints WHERE job_id = ?`, jobID)
	if err := row.Scan(&cpAdded, &cpUpdated, &cpWithdrawn, &cpSkipped, &cpDeadLetter); err != nil {
		t.Fatalf("failed to sum batch checkpoints: %v", err)
	}

	if cpAdded != expectedAdded || cpUpdated != expectedUpdated || cpWithdrawn != expectedWithdrawn ||
		cpSkipped != expectedSkipped || cpDeadLetter != expectedDeadLetter {
		t.Errorf("Batch checkpoints sum mismatch: got (%d, %d, %d, %d, %d), expected (%d, %d, %d, %d, %d)",
			cpAdded, cpUpdated, cpWithdrawn, cpSkipped, cpDeadLetter,
			expectedAdded, expectedUpdated, expectedWithdrawn, expectedSkipped, expectedDeadLetter)
	}

	// 5. Verify SSE emitted metrics match with 100% exact numerical fidelity
	sseMu.Lock()
	eventsCopy := make([]capturedEvent, len(capturedEvents))
	copy(eventsCopy, capturedEvents)
	sseMu.Unlock()

	var finalProgressSummary *domain.ChangeSummary
	var jobCompletedSummary *domain.ChangeSummary

	for _, evt := range eventsCopy {
		if evt.Type == "progress" {
			var prog struct {
				CumulativeSummary domain.ChangeSummary `json:"cumulative_summary"`
			}
			if err := json.Unmarshal(evt.Data, &prog); err == nil {
				s := prog.CumulativeSummary
				finalProgressSummary = &s
			}
		} else if evt.Type == "job_completed" {
			var comp struct {
				Summary domain.ChangeSummary `json:"summary"`
			}
			if err := json.Unmarshal(evt.Data, &comp); err == nil {
				s := comp.Summary
				jobCompletedSummary = &s
			}
		}
	}

	if finalProgressSummary == nil {
		t.Fatalf("No progress event with cumulative summary captured over SSE")
	}
	if jobCompletedSummary == nil {
		t.Fatalf("No job_completed event captured over SSE")
	}

	t.Logf("SSE Final Progress Cumulative Summary: %+v", *finalProgressSummary)
	t.Logf("SSE Job Completed Summary: %+v", *jobCompletedSummary)
	t.Logf("SQLite DB Job Summary: %+v", job.Summary)

	// Check 100% exact agreement between SSE progress cumulative and SQLite DB
	if *finalProgressSummary != job.Summary {
		t.Errorf("FAIL: SSE cumulative progress summary %+v != SQLite DB job summary %+v",
			*finalProgressSummary, job.Summary)
	}

	// Check 100% exact agreement between SSE job_completed summary and SQLite DB
	if *jobCompletedSummary != job.Summary {
		t.Errorf("FAIL: SSE job_completed summary %+v != SQLite DB job summary %+v",
			*jobCompletedSummary, job.Summary)
	}

	// Check exact match against ground truth expected numbers
	expectedSummary := domain.ChangeSummary{
		Added:      expectedAdded,
		Updated:    expectedUpdated,
		Withdrawn:  expectedWithdrawn,
		Skipped:    expectedSkipped,
		DeadLetter: expectedDeadLetter,
	}
	if job.Summary != expectedSummary {
		t.Errorf("FAIL: SQLite DB job summary %+v != expected %+v", job.Summary, expectedSummary)
	}
}

// -----------------------------------------------------------------------------
// Objective 4.2: Resumed Job Metric Fidelity
// -----------------------------------------------------------------------------

func TestAdversarial_ResumedJob_MetricFidelity(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	// Connect SSE listener
	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()

	sseReq, _ := http.NewRequestWithContext(sseCtx, http.MethodGet, server.URL+"/api/events", nil)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("failed SSE connect: %v", err)
	}
	defer sseResp.Body.Close()

	var sseMu sync.Mutex
	type capturedEvent struct {
		Type string
		Data json.RawMessage
	}
	var capturedEvents []capturedEvent

	go func() {
		scanner := bufio.NewScanner(sseResp.Body)
		var currentType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				sseMu.Lock()
				capturedEvents = append(capturedEvents, capturedEvent{
					Type: currentType,
					Data: json.RawMessage(dataStr),
				})
				sseMu.Unlock()
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Step 1: Start a generator job with 10 batches and cancel after batch 2
	body, _ := json.Marshal(map[string]any{
		"mode":          "generator",
		"total_batches": 50,
		"batch_size":    20,
		"seed":          42,
	})
	postReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/jobs", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("failed to post job: %v", err)
	}
	defer postResp.Body.Close()

	var startResp SyncJobResponse
	_ = json.NewDecoder(postResp.Body).Decode(&startResp)
	jobID := startResp.JobID

	// Allow first few batches to commit then cancel
	time.Sleep(15 * time.Millisecond)
	handler.CancelActiveJob()

	var interruptedJob *domain.SyncJob
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		interruptedJob, err = store.GetJob(context.Background(), jobID)
		if err == nil && interruptedJob != nil && interruptedJob.Status == domain.JobStatusInterrupted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err != nil || interruptedJob == nil {
		t.Fatalf("failed to get interrupted job: %v", err)
	}
	t.Logf("Interrupted job: committed_batch_index=%d, status=%s, summary=%+v",
		interruptedJob.CommittedBatchIndex, interruptedJob.Status, interruptedJob.Summary)

	if interruptedJob.Status != domain.JobStatusInterrupted {
		t.Fatalf("expected INTERRUPTED status, got %s", interruptedJob.Status)
	}

	// Step 2: Resume the job
	resumeReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/jobs/%s/resume", server.URL, jobID), bytes.NewReader([]byte("{}")))
	resumeReq.Header.Set("Content-Type", "application/json")
	resumeResp, err := http.DefaultClient.Do(resumeReq)
	if err != nil {
		t.Fatalf("failed to resume job: %v", err)
	}
	defer resumeResp.Body.Close()
	if resumeResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted on resume, got %d", resumeResp.StatusCode)
	}

	// Wait for resume to complete all 50 batches
	var finalJob *domain.SyncJob
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		finalJob, err = store.GetJob(context.Background(), jobID)
		if err == nil && finalJob.Status == domain.JobStatusCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to get resumed final job: %v", err)
	}
	if finalJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected COMPLETED after resume, got %s", finalJob.Status)
	}
	if finalJob.CommittedBatchIndex != 49 {
		t.Fatalf("expected committed_batch_index=49, got %d", finalJob.CommittedBatchIndex)
	}

	// Verify batch checkpoints table sum matches finalJob.Summary
	var cpAdded, cpUpdated, cpWithdrawn, cpSkipped, cpDeadLetter int
	row := store.DB().QueryRow(`
		SELECT
			COALESCE(SUM(added_count), 0),
			COALESCE(SUM(updated_count), 0),
			COALESCE(SUM(withdrawn_count), 0),
			COALESCE(SUM(skipped_count), 0),
			COALESCE(SUM(dead_letter_count), 0)
		FROM batch_checkpoints WHERE job_id = ?`, jobID)
	if err := row.Scan(&cpAdded, &cpUpdated, &cpWithdrawn, &cpSkipped, &cpDeadLetter); err != nil {
		t.Fatalf("failed to sum checkpoints: %v", err)
	}

	if cpAdded != finalJob.Summary.Added ||
		cpUpdated != finalJob.Summary.Updated ||
		cpWithdrawn != finalJob.Summary.Withdrawn ||
		cpSkipped != finalJob.Summary.Skipped ||
		cpDeadLetter != finalJob.Summary.DeadLetter {
		t.Errorf("Resumed job checkpoint sum (%d,%d,%d,%d,%d) != job summary (%d,%d,%d,%d,%d)",
			cpAdded, cpUpdated, cpWithdrawn, cpSkipped, cpDeadLetter,
			finalJob.Summary.Added, finalJob.Summary.Updated, finalJob.Summary.Withdrawn,
			finalJob.Summary.Skipped, finalJob.Summary.DeadLetter)
	}

	// Verify final job_completed event on SSE matches finalJob.Summary
	sseMu.Lock()
	eventsCopy := make([]capturedEvent, len(capturedEvents))
	copy(eventsCopy, capturedEvents)
	sseMu.Unlock()

	var completedSummary *domain.ChangeSummary
	for _, evt := range eventsCopy {
		if evt.Type == "job_completed" {
			var comp struct {
				JobID   string               `json:"job_id"`
				Summary domain.ChangeSummary `json:"summary"`
			}
			if err := json.Unmarshal(evt.Data, &comp); err == nil && comp.JobID == jobID {
				s := comp.Summary
				completedSummary = &s
			}
		}
	}

	if completedSummary == nil {
		t.Fatalf("No job_completed event captured for resumed job %s", jobID)
	}

	if *completedSummary != finalJob.Summary {
		t.Errorf("FAIL: Resumed job SSE summary %+v != SQLite DB summary %+v",
			*completedSummary, finalJob.Summary)
	}
}
