package sync_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"coderpush/pkg/domain"
	catalogSync "coderpush/pkg/sync"
)

func loadFixtureBatch(t *testing.T, path string) domain.Batch {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture file %s: %v", path, err)
	}

	var raw struct {
		JobID      string `json:"job_id"`
		BatchIndex int    `json:"batch_index"`
		Records    []struct {
			SKU       string  `json:"sku"`
			Name      string  `json:"name"`
			Price     float64 `json:"price"`
			Status    string  `json:"status"`
			Timestamp string  `json:"timestamp"`
		} `json:"records"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal JSON fixture %s: %v", path, err)
	}

	batch := domain.Batch{
		JobID:      raw.JobID,
		BatchIndex: raw.BatchIndex,
		Records:    make([]domain.SupplierUpdate, 0, len(raw.Records)),
	}

	for _, r := range raw.Records {
		ts, err := time.Parse(time.RFC3339, r.Timestamp)
		if err != nil {
			t.Fatalf("failed to parse timestamp %s in fixture %s: %v", r.Timestamp, path, err)
		}
		batch.Records = append(batch.Records, domain.SupplierUpdate{
			SKU:       r.SKU,
			Name:      r.Name,
			Price:     r.Price,
			Status:    domain.ProductStatus(r.Status),
			Timestamp: ts,
		})
	}

	return batch
}

func TestFixtureAudit_AllBatchesInvariants(t *testing.T) {
	mockDir := filepath.Join("..", "..", "mock_data")

	b1 := loadFixtureBatch(t, filepath.Join(mockDir, "batch_001_initial.json"))
	b2 := loadFixtureBatch(t, filepath.Join(mockDir, "batch_002_updates_and_idempotency.json"))
	b3 := loadFixtureBatch(t, filepath.Join(mockDir, "batch_003_anomalies_and_deadletter.json"))
	b4 := loadFixtureBatch(t, filepath.Join(mockDir, "batch_004_tombstone_reactivation_attempt.json"))

	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	// --- Step 1: Batch 1 Baseline Ingestion ---
	s1, err := engine.ExecuteBatch(ctx, b1)
	if err != nil {
		t.Fatalf("Batch 1 ExecuteBatch failed: %v", err)
	}
	expectedS1 := domain.ChangeSummary{Added: 10, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 0}
	if s1 != expectedS1 {
		t.Errorf("Batch 1 summary mismatch: got %+v, expected %+v", s1, expectedS1)
	}

	// Verify all 10 are active in storage
	for _, rec := range b1.Records {
		p, err := store.GetProduct(ctx, rec.SKU)
		if err != nil {
			t.Fatalf("GetProduct(%s) failed: %v", rec.SKU, err)
		}
		if p.Status != domain.StatusActive {
			t.Errorf("Product %s status got %s, expected %s", rec.SKU, p.Status, domain.StatusActive)
		}
		if p.Price != rec.Price {
			t.Errorf("Product %s price got %f, expected %f", rec.SKU, p.Price, rec.Price)
		}
	}

	// --- Step 2: Batch 2 Updates, Idempotency & Tombstones ---
	s2, err := engine.ExecuteBatch(ctx, b2)
	if err != nil {
		t.Fatalf("Batch 2 ExecuteBatch failed: %v", err)
	}
	expectedS2 := domain.ChangeSummary{Added: 0, Updated: 5, Withdrawn: 2, Skipped: 3, DeadLetter: 0}
	if s2 != expectedS2 {
		t.Errorf("Batch 2 summary mismatch: got %+v, expected %+v", s2, expectedS2)
	}

	// Verify tombstoned products
	for _, sku := range []string{"SKU-COF-005", "SKU-EQU-009"} {
		p, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("GetProduct(%s) failed: %v", sku, err)
		}
		if p.Status != domain.StatusWithdrawn {
			t.Errorf("Invariant C violation: Product %s status got %s, expected %s", sku, p.Status, domain.StatusWithdrawn)
		}
	}

	// Verify replayed products (Invariant A) did not drift
	for _, sku := range []string{"SKU-COF-004", "SKU-EQU-008", "SKU-ACC-010"} {
		p, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("GetProduct(%s) failed: %v", sku, err)
		}
		expectedTime, _ := time.Parse(time.RFC3339, "2026-10-21T10:00:00Z")
		if !p.SupplierTimestamp.Equal(expectedTime) {
			t.Errorf("Invariant A violation: Product %s supplier timestamp drifted: got %v, expected %v", sku, p.SupplierTimestamp, expectedTime)
		}
	}

	// --- Step 3: Batch 3 Clock Skew Skips & Dead-Letter Quarantine ---
	s3, err := engine.ExecuteBatch(ctx, b3)
	if err != nil {
		t.Fatalf("Batch 3 ExecuteBatch failed: %v", err)
	}
	expectedS3 := domain.ChangeSummary{Added: 1, Updated: 1, Withdrawn: 0, Skipped: 2, DeadLetter: 3}
	if s3 != expectedS3 {
		t.Errorf("Batch 3 summary mismatch: got %+v, expected %+v", s3, expectedS3)
	}

	// Invariant B: Clock skew records must not overwrite catalog
	// SKU-COF-001 price should still be 19.95 (from batch 2, 11:00:00Z), NOT 15.00 (from batch 3, 09:00:00Z)
	p001, err := store.GetProduct(ctx, "SKU-COF-001")
	if err != nil {
		t.Fatalf("GetProduct(SKU-COF-001) failed: %v", err)
	}
	if p001.Price != 19.95 {
		t.Errorf("Invariant B violation: SKU-COF-001 price overwritten by older timestamp: got %f, expected 19.95", p001.Price)
	}

	// Invariant D: Sibling isolation - SKU-COF-011 added and SKU-COF-002 updated
	p011, err := store.GetProduct(ctx, "SKU-COF-011")
	if err != nil {
		t.Fatalf("GetProduct(SKU-COF-011) failed: %v", err)
	}
	if p011.Price != 38.00 {
		t.Errorf("Invariant D violation: sibling product SKU-COF-011 not inserted: got %f, expected 38.00", p011.Price)
	}

	// Verify dead letters in storage
	dlList, totalDL, err := store.ListDeadLetterRecords(ctx, "", "", 10, 0)
	if err != nil {
		t.Fatalf("ListDeadLetterRecords failed: %v", err)
	}
	if totalDL != 3 || len(dlList) != 3 {
		t.Errorf("Invariant D dead letter count mismatch: got %d (list len %d), expected 3", totalDL, len(dlList))
	}

	// --- Step 4: Batch 4 Tombstone Reactivation Attempt ---
	s4, err := engine.ExecuteBatch(ctx, b4)
	if err != nil {
		t.Fatalf("Batch 4 ExecuteBatch failed: %v", err)
	}
	expectedS4 := domain.ChangeSummary{Added: 0, Updated: 1, Withdrawn: 0, Skipped: 2, DeadLetter: 0}
	if s4 != expectedS4 {
		t.Errorf("Batch 4 summary mismatch: got %+v, expected %+v", s4, expectedS4)
	}

	// Invariant C: Tombstones must NOT be resurrected
	for _, sku := range []string{"SKU-COF-005", "SKU-EQU-009"} {
		p, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("GetProduct(%s) failed: %v", sku, err)
		}
		if p.Status != domain.StatusWithdrawn {
			t.Errorf("CRITICAL Invariant C violation: Tombstone %s resurrected to %s!", sku, p.Status)
		}
	}

	// Valid update in batch 4 (SKU-COF-003) must succeed
	p003, err := store.GetProduct(ctx, "SKU-COF-003")
	if err != nil {
		t.Fatalf("GetProduct(SKU-COF-003) failed: %v", err)
	}
	if p003.Price != 18.00 {
		t.Errorf("Batch 4 valid update failed: SKU-COF-003 price got %f, expected 18.00", p003.Price)
	}
}

func TestFixtureAudit_NDJSONStreamingRunJob(t *testing.T) {
	mockDir := filepath.Join("..", "..", "mock_data")
	ndjsonPath := filepath.Join(mockDir, "all_batches.ndjson")
	data, err := os.ReadFile(ndjsonPath)
	if err != nil {
		t.Fatalf("failed reading %s: %v", ndjsonPath, err)
	}

	lines := splitLines(data)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines in ndjson, got %d", len(lines))
	}

	batchChan := make(chan domain.Batch, len(lines))
	for idx, line := range lines {
		var raw struct {
			JobID      string `json:"job_id"`
			BatchIndex int    `json:"batch_index"`
			Records    []struct {
				SKU       string  `json:"sku"`
				Name      string  `json:"name"`
				Price     float64 `json:"price"`
				Status    string  `json:"status"`
				Timestamp string  `json:"timestamp"`
			} `json:"records"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("failed unmarshaling line %d: %v", idx, err)
		}
		b := domain.Batch{
			JobID:      "job-ndjson-audit",
			BatchIndex: raw.BatchIndex,
			Records:    make([]domain.SupplierUpdate, 0, len(raw.Records)),
		}
		for _, r := range raw.Records {
			ts, err := time.Parse(time.RFC3339, r.Timestamp)
			if err != nil {
				t.Fatalf("failed parsing timestamp %s: %v", r.Timestamp, err)
			}
			b.Records = append(b.Records, domain.SupplierUpdate{
				SKU:       r.SKU,
				Name:      r.Name,
				Price:     r.Price,
				Status:    domain.ProductStatus(r.Status),
				Timestamp: ts,
			})
		}
		batchChan <- b
	}
	close(batchChan)

	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	job, err := engine.RunJob(ctx, "job-ndjson-audit", batchChan)
	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if job.Status != domain.JobStatusCompleted {
		t.Errorf("job status got %s, expected %s", job.Status, domain.JobStatusCompleted)
	}
	if job.CommittedBatchIndex != 3 {
		t.Errorf("job committed batch index got %d, expected 3", job.CommittedBatchIndex)
	}

	// Cumulative summary across all 4 batches:
	// Added: 10 + 0 + 1 + 0 = 11
	// Updated: 0 + 5 + 1 + 1 = 7
	// Withdrawn: 0 + 2 + 0 + 0 = 2
	// Skipped: 0 + 3 + 2 + 2 = 7
	// DeadLetter: 0 + 0 + 3 + 0 = 3
	expectedSummary := domain.ChangeSummary{
		Added:      11,
		Updated:    7,
		Withdrawn:  2,
		Skipped:    7,
		DeadLetter: 3,
	}
	if job.Summary != expectedSummary {
		t.Errorf("cumulative summary mismatch: got %+v, expected %+v", job.Summary, expectedSummary)
	}
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
