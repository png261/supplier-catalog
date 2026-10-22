package e2e_test

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestE2E_SSEEventStream(t *testing.T) {
	ts := createE2EServer(t)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("failed to create SSE request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	receivedKeepaliveOrEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "retry:") || strings.HasPrefix(line, ": ping") || strings.HasPrefix(line, "event:") {
			receivedKeepaliveOrEvent = true
			break
		}
	}
	if !receivedKeepaliveOrEvent {
		t.Errorf("expected SSE event frame or keepalive")
	}
}
