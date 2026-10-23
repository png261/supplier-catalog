package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/domain"
	_ "modernc.org/sqlite"
)

// =============================================================================
// CHALLENGE 1: MULTI-TENANT CROSS-TALK ATTACK
// =============================================================================
// Interleaved concurrent requests across multiple tenant shops (shop-alpha,
// shop-beta, shop-gamma) streaming events over persistent SSE connections.
// Asserts 100% data segregation, zero cross-tenant query visibility,
// zero cross-tenant SSE event delivery, and rejection of cross-tenant actions.
// =============================================================================
func TestChallenger_MultiTenant_CrossTalk_Attack(t *testing.T) {
	ts, _, broker, tempDir := setupVersioningE2EServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	shops := []string{"shop-alpha", "shop-beta", "shop-gamma"}

	// 1. Establish persistent SSE streams for all 3 active shops + 1 idle shop (shop-delta)
	eventChans := make(map[string]<-chan sseMessage)
	cleanups := make([]func(), 0, 4)
	t.Cleanup(func() {
		for _, fn := range cleanups {
			fn()
		}
	})

	for _, shop := range append(shops, "shop-delta") {
		ch, cleanup := startSSEListener(ctx, t, ts.URL, shop)
		eventChans[shop] = ch
		cleanups = append(cleanups, cleanup)
	}

	// Wait briefly to allow SSE listeners to establish and register on tenant brokers
	time.Sleep(100 * time.Millisecond)

	// Thread-safe collectors for created versions
	var verMu sync.Mutex
	createdVersions := make(map[string][]string) // shop -> []versionID
	for _, shop := range shops {
		createdVersions[shop] = make([]string, 0)
	}

	// 2. Launch 18 concurrent workers (6 workers per shop)
	workersPerShop := 6
	iterationsPerWorker := 3
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errChan := make(chan error, workersPerShop*len(shops)*iterationsPerWorker*10)

	for _, shopID := range shops {
		for w := 0; w < workersPerShop; w++ {
			wg.Add(1)
			go func(shop string, workerID int) {
				defer wg.Done()
				<-startBarrier

				client := ts.Client()
				prefix := strings.ToUpper(strings.TrimPrefix(shop, "shop-")) // ALPHA, BETA, GAMMA

				for it := 0; it < iterationsPerWorker; it++ {
					now := time.Now().UTC()
					sku1 := fmt.Sprintf("%s-SKU-W%d-IT%d-1", prefix, workerID, it)
					sku2 := fmt.Sprintf("%s-SKU-W%d-IT%d-2", prefix, workerID, it)

					// A. POST /api/patches/publish
					publishReq := domain.CatalogPatch{
						Author:      fmt.Sprintf("%s-worker-%d", shop, workerID),
						Description: fmt.Sprintf("[%s] Patch commit iteration %d by worker %d", shop, it, workerID),
						Items: []domain.PatchItem{
							{
								SKU:       sku1,
								Name:      fmt.Sprintf("%s Item W%d IT%d A", prefix, workerID, it),
								Price:     float64(10 + workerID*10 + it),
								Status:    domain.StatusActive,
								Timestamp: &now,
							},
							{
								SKU:       sku2,
								Name:      fmt.Sprintf("%s Item W%d IT%d B", prefix, workerID, it),
								Price:     float64(20 + workerID*10 + it),
								Status:    domain.StatusActive,
								Timestamp: &now,
							},
						},
					}

					bodyBytes, _ := json.Marshal(publishReq)
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(bodyBytes))
					if err != nil {
						errChan <- fmt.Errorf("[%s] failed creating publish request: %w", shop, err)
						return
					}
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Shop-Id", shop)

					resp, err := client.Do(req)
					if err != nil {
						errChan <- fmt.Errorf("[%s] publish request failed: %w", shop, err)
						return
					}
					if resp.StatusCode != http.StatusCreated {
						respBody, _ := io.ReadAll(resp.Body)
						resp.Body.Close()
						errChan <- fmt.Errorf("[%s] expected 201 Created on publish, got %d: %s", shop, resp.StatusCode, string(respBody))
						return
					}

					var pubResp api.PatchPublishResponse
					if err := json.NewDecoder(resp.Body).Decode(&pubResp); err != nil {
						resp.Body.Close()
						errChan <- fmt.Errorf("[%s] failed decoding publish response: %w", shop, err)
						return
					}
					resp.Body.Close()

					verMu.Lock()
					createdVersions[shop] = append(createdVersions[shop], pubResp.Version.VersionID)
					verMu.Unlock()

					// B. POST /api/patches/preview (Dry-run preview)
					later := now.Add(time.Minute)
					previewReq := domain.CatalogPatch{
						Items: []domain.PatchItem{
							{
								SKU:       sku1,
								Name:      fmt.Sprintf("%s Item W%d IT%d A Updated", prefix, workerID, it),
								Price:     float64(99.99),
								Status:    domain.StatusActive,
								Timestamp: &later,
							},
						},
					}
					prevBytes, _ := json.Marshal(previewReq)
					prevHTTPReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/preview", bytes.NewReader(prevBytes))
					prevHTTPReq.Header.Set("Content-Type", "application/json")
					prevHTTPReq.Header.Set("X-Shop-Id", shop)
					prevResp, err := client.Do(prevHTTPReq)
					if err != nil {
						errChan <- fmt.Errorf("[%s] preview request failed: %w", shop, err)
						return
					}
					if prevResp.StatusCode != http.StatusOK {
						prevResp.Body.Close()
						errChan <- fmt.Errorf("[%s] preview expected 200, got %d", shop, prevResp.StatusCode)
						return
					}
					prevResp.Body.Close()

					// C. GET /api/versions
					verListReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/versions?limit=5", nil)
					verListReq.Header.Set("X-Shop-Id", shop)
					verListResp, err := client.Do(verListReq)
					if err != nil {
						errChan <- fmt.Errorf("[%s] list versions failed: %w", shop, err)
						return
					}
					if verListResp.StatusCode != http.StatusOK {
						verListResp.Body.Close()
						errChan <- fmt.Errorf("[%s] list versions expected 200, got %d", shop, verListResp.StatusCode)
						return
					}
					var vList api.VersionListResponse
					_ = json.NewDecoder(verListResp.Body).Decode(&vList)
					verListResp.Body.Close()

					// Verify all listed versions for this shop belong to this shop
					for _, v := range vList.Versions {
						if !strings.Contains(v.Description, fmt.Sprintf("[%s]", shop)) && !strings.Contains(v.Author, shop) {
							errChan <- fmt.Errorf("[%s] leak: received version belonging to another shop: desc=%q author=%q", shop, v.Description, v.Author)
							return
						}
					}

					// D. GET /api/products?status=ACTIVE
					prodReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/products?status=ACTIVE&limit=100", nil)
					prodReq.Header.Set("X-Shop-Id", shop)
					prodResp, err := client.Do(prodReq)
					if err != nil {
						errChan <- fmt.Errorf("[%s] list products failed: %w", shop, err)
						return
					}
					if prodResp.StatusCode != http.StatusOK {
						prodResp.Body.Close()
						errChan <- fmt.Errorf("[%s] list products expected 200, got %d", shop, prodResp.StatusCode)
						return
					}
					var pList api.ProductListResponse
					_ = json.NewDecoder(prodResp.Body).Decode(&pList)
					prodResp.Body.Close()

					for _, p := range pList.Products {
						if !strings.HasPrefix(p.SKU, prefix+"-") {
							errChan <- fmt.Errorf("[%s] CRITICAL CROSS-TALK: product %s visible in %s!", shop, p.SKU, shop)
							return
						}
					}
				}
			}(shopID, w)
		}
	}

	// Release the concurrent barrier!
	close(startBarrier)
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Fatalf("Concurrent worker error: %v", err)
	}

	// 3. Unauthorized Cross-Tenant Probes:
	// Verify that each shop is strictly barred from accessing other shops' versions or data.
	client := ts.Client()

	verMu.Lock()
	alphaVerID := createdVersions["shop-alpha"][0]
	betaVerID := createdVersions["shop-beta"][0]
	gammaVerID := createdVersions["shop-gamma"][0]
	verMu.Unlock()

	// Probe A: shop-alpha attempts to inspect shop-beta's version diff -> MUST 404
	reqDiff, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/versions/%s/diff", ts.URL, betaVerID), nil)
	reqDiff.Header.Set("X-Shop-Id", "shop-alpha")
	respDiff, err := client.Do(reqDiff)
	if err != nil {
		t.Fatalf("Probe A failed: %v", err)
	}
	if respDiff.StatusCode != http.StatusNotFound {
		t.Fatalf("CROSS-TALK SECURITY BREACH: shop-alpha accessed shop-beta diff %s! Got HTTP %d, expected 404", betaVerID, respDiff.StatusCode)
	}
	respDiff.Body.Close()

	// Probe B: shop-beta attempts to rollback shop-gamma's version -> MUST 404
	rollBody, _ := json.Marshal(api.RollbackRequest{Author: "attacker", Description: "unauthorized rollback"})
	reqRoll, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/versions/%s/rollback", ts.URL, gammaVerID), bytes.NewReader(rollBody))
	reqRoll.Header.Set("Content-Type", "application/json")
	reqRoll.Header.Set("X-Shop-Id", "shop-beta")
	respRoll, err := client.Do(reqRoll)
	if err != nil {
		t.Fatalf("Probe B failed: %v", err)
	}
	if respRoll.StatusCode != http.StatusNotFound {
		t.Fatalf("CROSS-TALK SECURITY BREACH: shop-beta executed rollback on shop-gamma version %s! Got HTTP %d, expected 404", gammaVerID, respRoll.StatusCode)
	}
	respRoll.Body.Close()

	// Probe C: shop-gamma attempts to inspect shop-alpha's version diff -> MUST 404
	reqDiffC, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/versions/%s/diff", ts.URL, alphaVerID), nil)
	reqDiffC.Header.Set("X-Shop-Id", "shop-gamma")
	respDiffC, err := client.Do(reqDiffC)
	if err != nil {
		t.Fatalf("Probe C failed: %v", err)
	}
	if respDiffC.StatusCode != http.StatusNotFound {
		t.Fatalf("CROSS-TALK SECURITY BREACH: shop-gamma accessed shop-alpha diff %s! Got HTTP %d, expected 404", alphaVerID, respDiffC.StatusCode)
	}
	respDiffC.Body.Close()

	// Probe D: Cross-tenant search queries:
	// shop-alpha queries for BETA items -> must return 0 items
	reqSearch, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/products?search=BETA", nil)
	reqSearch.Header.Set("X-Shop-Id", "shop-alpha")
	respSearch, err := client.Do(reqSearch)
	if err != nil {
		t.Fatalf("Probe D failed: %v", err)
	}
	var sList api.ProductListResponse
	_ = json.NewDecoder(respSearch.Body).Decode(&sList)
	respSearch.Body.Close()
	if sList.Total != 0 || len(sList.Products) != 0 {
		t.Fatalf("CROSS-TALK QUERY LEAK: shop-alpha searched for BETA and found %d items!", sList.Total)
	}

	// 4. Perform an authorized rollback on each shop
	for _, shop := range shops {
		verMu.Lock()
		targetV := createdVersions[shop][0]
		verMu.Unlock()

		rbPayload, _ := json.Marshal(api.RollbackRequest{
			Author:      fmt.Sprintf("%s-admin", shop),
			Description: fmt.Sprintf("[%s] Authorized rollback to %s", shop, targetV),
		})
		rbReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/versions/%s/rollback", ts.URL, targetV), bytes.NewReader(rbPayload))
		rbReq.Header.Set("Content-Type", "application/json")
		rbReq.Header.Set("X-Shop-Id", shop)

		rbResp, err := client.Do(rbReq)
		if err != nil {
			t.Fatalf("[%s] rollback failed: %v", shop, err)
		}
		if rbResp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(rbResp.Body)
			rbResp.Body.Close()
			t.Fatalf("[%s] rollback expected 200, got %d: %s", shop, rbResp.StatusCode, string(b))
		}
		rbResp.Body.Close()
	}

	// Wait briefly for all SSE broadcasts to flush
	time.Sleep(300 * time.Millisecond)

	// 5. Audit persistent SSE event streams for zero cross-talk
	auditSSEStream := func(shop string, ch <-chan sseMessage) {
		expectedPrefix := strings.ToUpper(strings.TrimPrefix(shop, "shop-"))
		foreign1 := "ALPHA"
		foreign2 := "BETA"
		if expectedPrefix == "ALPHA" {
			foreign1, foreign2 = "BETA", "GAMMA"
		} else if expectedPrefix == "BETA" {
			foreign1, foreign2 = "ALPHA", "GAMMA"
		}

		eventCount := 0
		drainTimer := time.NewTimer(500 * time.Millisecond)
		defer drainTimer.Stop()

	drainLoop:
		for {
			select {
			case msg := <-ch:
				if msg.eventType == "version_created" || msg.eventType == "version_rollback" {
					eventCount++
					// Verify data does not contain foreign shop tags
					if strings.Contains(msg.data, foreign1) {
						t.Fatalf("CRITICAL SSE CROSS-TALK: %s received SSE event with data from %s: %s", shop, foreign1, msg.data)
					}
					if strings.Contains(msg.data, foreign2) {
						t.Fatalf("CRITICAL SSE CROSS-TALK: %s received SSE event with data from %s: %s", shop, foreign2, msg.data)
					}
				}
			case <-drainTimer.C:
				break drainLoop
			}
		}

		if eventCount == 0 {
			t.Fatalf("[%s] expected to receive SSE version events, got 0", shop)
		}
	}

	auditSSEStream("shop-alpha", eventChans["shop-alpha"])
	auditSSEStream("shop-beta", eventChans["shop-beta"])
	auditSSEStream("shop-gamma", eventChans["shop-gamma"])

	// 6. Audit idle shop-delta SSE stream: must have received ZERO version events!
	deltaTimer := time.NewTimer(300 * time.Millisecond)
	defer deltaTimer.Stop()
deltaLoop:
	for {
		select {
		case msg := <-eventChans["shop-delta"]:
			if msg.eventType == "version_created" || msg.eventType == "version_rollback" {
				t.Fatalf("CRITICAL SSE LEAK: idle shop-delta received version event: type=%s data=%s", msg.eventType, msg.data)
			}
		case <-deltaTimer.C:
			break deltaLoop
		}
	}

	// 7. Directly inspect physical SQLite database files on disk
	shopsDir := filepath.Join(tempDir, "shops")
	for _, shop := range shops {
		dbFile := filepath.Join(shopsDir, fmt.Sprintf("%s.db", shop))
		db, err := sql.Open("sqlite", dbFile)
		if err != nil {
			t.Fatalf("failed opening shop db %s: %v", dbFile, err)
		}

		// Check products table
		rows, err := db.Query("SELECT sku FROM products;")
		if err != nil {
			db.Close()
			t.Fatalf("failed querying products from %s: %v", shop, err)
		}
		prefix := strings.ToUpper(strings.TrimPrefix(shop, "shop-"))
		prodCount := 0
		for rows.Next() {
			var sku string
			_ = rows.Scan(&sku)
			prodCount++
			if !strings.HasPrefix(sku, prefix+"-") {
				rows.Close()
				db.Close()
				t.Fatalf("PHYSICAL STORAGE CROSS-CONTAMINATION: Shop %s contains foreign product SKU %s!", shop, sku)
			}
		}
		rows.Close()

		if prodCount == 0 {
			db.Close()
			t.Fatalf("Shop %s has 0 products in db!", shop)
		}

		// Check catalog_versions table
		vRows, err := db.Query("SELECT description, author FROM catalog_versions;")
		if err != nil {
			db.Close()
			t.Fatalf("failed querying versions from %s: %v", shop, err)
		}
		vCount := 0
		for vRows.Next() {
			var desc, author string
			_ = vRows.Scan(&desc, &author)
			vCount++
			if !strings.Contains(desc, fmt.Sprintf("[%s]", shop)) && !strings.Contains(author, shop) {
				vRows.Close()
				db.Close()
				t.Fatalf("PHYSICAL STORAGE CROSS-CONTAMINATION: Shop %s contains foreign version: desc=%q, author=%q", shop, desc, author)
			}
		}
		vRows.Close()
		db.Close()

		if vCount == 0 {
			t.Fatalf("Shop %s has 0 versions in db!", shop)
		}
	}

	// Suppress unused variable warning for broker
	_ = broker
}

// =============================================================================
// CHALLENGE 2: DEEP CHAINED ROLLBACK ATTACK & TOMBSTONE INVARIANT VERIFICATION
// =============================================================================
// Chain: V1 -> V2 -> V3 -> V4 -> V5 -> Rollback V1 -> Rollback V3 -> Rollback V1
// -> Rollback V5 -> Rollback V6 (Rollback of a Rollback).
// Asserts that under NO sequence of historical rollbacks is a WITHDRAWN
// tombstone ever resurrected to ACTIVE in the live catalog or in active queries.
// =============================================================================
func TestChallenger_DeepChainedRollback_TombstoneResurrectionInvariant(t *testing.T) {
	ts, _, _, _ := setupVersioningE2EServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := ts.Client()
	shopID := "shop-tombstone-deep"

	publishPatch := func(author, desc string, items []domain.PatchItem) api.PatchPublishResponse {
		t.Helper()
		patch := domain.CatalogPatch{
			Author:      author,
			Description: desc,
			Items:       items,
		}
		bodyBytes, _ := json.Marshal(patch)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(bodyBytes))
		if err != nil {
			t.Fatalf("failed creating patch request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Shop-Id", shopID)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed executing patch: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201 Created on publish, got %d: %s", resp.StatusCode, string(body))
		}

		var pubResp api.PatchPublishResponse
		if err := json.NewDecoder(resp.Body).Decode(&pubResp); err != nil {
			t.Fatalf("failed decoding publish response: %v", err)
		}
		return pubResp
	}

	rollbackTo := func(versionID, author, desc string) api.RollbackResponse {
		t.Helper()
		reqBody, _ := json.Marshal(api.RollbackRequest{
			Author:      author,
			Description: desc,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/versions/%s/rollback", ts.URL, versionID), bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("failed creating rollback request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Shop-Id", shopID)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed executing rollback: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 OK on rollback to %s, got %d: %s", versionID, resp.StatusCode, string(body))
		}

		var rbResp api.RollbackResponse
		if err := json.NewDecoder(resp.Body).Decode(&rbResp); err != nil {
			t.Fatalf("failed decoding rollback response: %v", err)
		}
		return rbResp
	}

	getActiveProducts := func() map[string]domain.Product {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/products?status=ACTIVE&limit=100", nil)
		if err != nil {
			t.Fatalf("failed creating list products request: %v", err)
		}
		req.Header.Set("X-Shop-Id", shopID)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed listing products: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		var listResp api.ProductListResponse
		if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
			t.Fatalf("failed decoding product list: %v", err)
		}

		m := make(map[string]domain.Product)
		for _, p := range listResp.Products {
			m[p.SKU] = p
		}
		return m
	}

	getAllProducts := func() map[string]domain.Product {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/products?limit=100", nil)
		if err != nil {
			t.Fatalf("failed creating all products request: %v", err)
		}
		req.Header.Set("X-Shop-Id", shopID)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed listing all products: %v", err)
		}
		defer resp.Body.Close()

		var listResp api.ProductListResponse
		_ = json.NewDecoder(resp.Body).Decode(&listResp)

		m := make(map[string]domain.Product)
		for _, p := range listResp.Products {
			m[p.SKU] = p
		}
		return m
	}

	t0 := time.Now().UTC().Truncate(time.Second)

	// -------------------------------------------------------------------------
	// Step 1: Version 1 (V1) - Baseline Seed
	// -------------------------------------------------------------------------
	tV1 := t0.Add(-50 * time.Minute)
	v1Resp := publishPatch("admin", "V1: Initial seeding", []domain.PatchItem{
		{SKU: "SKU-A", Name: "Product A", Price: 10.00, Status: domain.StatusActive, Timestamp: &tV1},
		{SKU: "SKU-B", Name: "Product B", Price: 20.00, Status: domain.StatusActive, Timestamp: &tV1},
		{SKU: "SKU-TOMB1", Name: "Tombstone Alpha", Price: 30.00, Status: domain.StatusActive, Timestamp: &tV1},
		{SKU: "SKU-TOMB2", Name: "Tombstone Beta", Price: 40.00, Status: domain.StatusActive, Timestamp: &tV1},
	})
	v1ID := v1Resp.Version.VersionID

	// -------------------------------------------------------------------------
	// Step 2: Version 2 (V2) - Update A, Add C
	// -------------------------------------------------------------------------
	tV2 := t0.Add(-40 * time.Minute)
	v2Resp := publishPatch("admin", "V2: Update A, Add C", []domain.PatchItem{
		{SKU: "SKU-A", Name: "Product A v2", Price: 15.00, Status: domain.StatusActive, Timestamp: &tV2},
		{SKU: "SKU-C", Name: "Product C", Price: 50.00, Status: domain.StatusActive, Timestamp: &tV2},
	})
	_ = v2Resp

	// -------------------------------------------------------------------------
	// Step 3: Version 3 (V3) - WITHDRAW SKU-TOMB1, Update B
	// -------------------------------------------------------------------------
	tV3 := t0.Add(-30 * time.Minute)
	v3Resp := publishPatch("admin", "V3: Withdraw TOMB1", []domain.PatchItem{
		{SKU: "SKU-TOMB1", Name: "Tombstone Alpha", Price: 30.00, Status: domain.StatusWithdrawn, Timestamp: &tV3},
		{SKU: "SKU-B", Name: "Product B v3", Price: 25.00, Status: domain.StatusActive, Timestamp: &tV3},
	})
	v3ID := v3Resp.Version.VersionID

	// -------------------------------------------------------------------------
	// Step 4: Version 4 (V4) - WITHDRAW SKU-TOMB2, Add D, Update A
	// -------------------------------------------------------------------------
	tV4 := t0.Add(-20 * time.Minute)
	v4Resp := publishPatch("admin", "V4: Withdraw TOMB2, Add D", []domain.PatchItem{
		{SKU: "SKU-TOMB2", Name: "Tombstone Beta", Price: 40.00, Status: domain.StatusWithdrawn, Timestamp: &tV4},
		{SKU: "SKU-D", Name: "Product D", Price: 70.00, Status: domain.StatusActive, Timestamp: &tV4},
		{SKU: "SKU-A", Name: "Product A v4", Price: 18.00, Status: domain.StatusActive, Timestamp: &tV4},
	})
	_ = v4Resp

	// -------------------------------------------------------------------------
	// Step 5: Version 5 (V5) - Update B, Add E
	// -------------------------------------------------------------------------
	tV5 := t0.Add(-10 * time.Minute)
	v5Resp := publishPatch("admin", "V5: Update B, Add E", []domain.PatchItem{
		{SKU: "SKU-B", Name: "Product B v5", Price: 28.00, Status: domain.StatusActive, Timestamp: &tV5},
		{SKU: "SKU-E", Name: "Product E", Price: 90.00, Status: domain.StatusActive, Timestamp: &tV5},
	})
	v5ID := v5Resp.Version.VersionID

	// Verify state before rollbacks:
	// Active items must be: SKU-A, SKU-B, SKU-C, SKU-D, SKU-E (5 items)
	// Withdrawn items must be: SKU-TOMB1, SKU-TOMB2 (2 items)
	preActive := getActiveProducts()
	if len(preActive) != 5 {
		t.Fatalf("expected 5 active items at V5, got %d", len(preActive))
	}
	if _, exists := preActive["SKU-TOMB1"]; exists {
		t.Fatalf("SKU-TOMB1 should not be active at V5")
	}
	if _, exists := preActive["SKU-TOMB2"]; exists {
		t.Fatalf("SKU-TOMB2 should not be active at V5")
	}

	// -------------------------------------------------------------------------
	// Adversarial Probe: Attempt direct resurrection via POST /api/patches/publish
	// -------------------------------------------------------------------------
	resurrectPayload := domain.CatalogPatch{
		Author:      "malicious-attacker",
		Description: "Attempt to resurrect SKU-TOMB1",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-TOMB1",
				Name:      "Resurrected Tombstone",
				Price:     35.00,
				Status:    domain.StatusActive,
				Timestamp: &t0,
			},
		},
	}
	resBytes, _ := json.Marshal(resurrectPayload)
	resReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(resBytes))
	resReq.Header.Set("Content-Type", "application/json")
	resReq.Header.Set("X-Shop-Id", shopID)

	resResp, err := client.Do(resReq)
	if err != nil {
		t.Fatalf("resurrection probe failed: %v", err)
	}
	if resResp.StatusCode != http.StatusUnprocessableEntity {
		resResp.Body.Close()
		t.Fatalf("INVARIANT VIOLATION: direct resurrection returned %d, expected 422 Unprocessable Entity", resResp.StatusCode)
	}
	resResp.Body.Close()

	// -------------------------------------------------------------------------
	// Deep Historical Rollback 1: Rollback to V1 (Creating Version 6)
	// Target V1 has SKU-TOMB1 and SKU-TOMB2 as ACTIVE!
	// Invariant A Requirement:
	// - Neither SKU-TOMB1 nor SKU-TOMB2 may be resurrected! They must remain WITHDRAWN.
	// - Items added after V1 (SKU-C, SKU-D, SKU-E) must be transition-withdrawn!
	// - SKU-A and SKU-B prices revert to V1 ($10.00, $20.00).
	// - Exactly SKU-A and SKU-B remain ACTIVE!
	// -------------------------------------------------------------------------
	v6Resp := rollbackTo(v1ID, "rollback-ops", "Rollback 1 -> V1")
	v6ID := v6Resp.Version.VersionID

	activeAfterV6 := getActiveProducts()
	if len(activeAfterV6) != 2 {
		t.Fatalf("ROLLBACK 1 VIOLATION: expected exactly 2 active items after rollback to V1, got %d: %+v", len(activeAfterV6), activeAfterV6)
	}
	if pA, ok := activeAfterV6["SKU-A"]; !ok || pA.Price != 10.00 {
		t.Fatalf("SKU-A price should have reverted to 10.00, got %+v", pA)
	}
	if pB, ok := activeAfterV6["SKU-B"]; !ok || pB.Price != 20.00 {
		t.Fatalf("SKU-B price should have reverted to 20.00, got %+v", pB)
	}

	allAfterV6 := getAllProducts()
	for _, tombSKU := range []string{"SKU-TOMB1", "SKU-TOMB2", "SKU-C", "SKU-D", "SKU-E"} {
		p, exists := allAfterV6[tombSKU]
		if !exists {
			t.Fatalf("Item %s missing from catalog table!", tombSKU)
		}
		if p.Status != domain.StatusWithdrawn {
			t.Fatalf("CRITICAL INVARIANT A VIOLATION: %s has status %s after rollback to V1 (expected WITHDRAWN)!", tombSKU, p.Status)
		}
	}

	// -------------------------------------------------------------------------
	// Deep Historical Rollback 2: Rollback to V3 (Creating Version 7)
	// Target V3 had:
	// - SKU-TOMB1 as WITHDRAWN
	// - SKU-TOMB2 as ACTIVE (was withdrawn in V4)
	// - SKU-C as ACTIVE (was added in V2, withdrawn in Rollback 1)
	// - SKU-A as ACTIVE ($15.00)
	// - SKU-B as ACTIVE ($25.00)
	// Invariant A Requirement:
	// - SKU-TOMB2 was WITHDRAWN in live catalog -> MUST NOT BE RESURRECTED!
	// - SKU-C was WITHDRAWN in live catalog -> MUST NOT BE RESURRECTED!
	// - Exactly SKU-A and SKU-B remain ACTIVE!
	// -------------------------------------------------------------------------
	v7Resp := rollbackTo(v3ID, "rollback-ops", "Rollback 2 -> V3")
	_ = v7Resp

	activeAfterV7 := getActiveProducts()
	if len(activeAfterV7) != 2 {
		t.Fatalf("ROLLBACK 2 VIOLATION: expected exactly 2 active items after rollback to V3, got %d: %+v", len(activeAfterV7), activeAfterV7)
	}
	if pA, ok := activeAfterV7["SKU-A"]; !ok || pA.Price != 15.00 {
		t.Fatalf("SKU-A price should be 15.00 (from V3), got %+v", pA)
	}
	if pB, ok := activeAfterV7["SKU-B"]; !ok || pB.Price != 25.00 {
		t.Fatalf("SKU-B price should be 25.00 (from V3), got %+v", pB)
	}

	allAfterV7 := getAllProducts()
	for _, tombSKU := range []string{"SKU-TOMB1", "SKU-TOMB2", "SKU-C", "SKU-D", "SKU-E"} {
		p, exists := allAfterV7[tombSKU]
		if !exists || p.Status != domain.StatusWithdrawn {
			t.Fatalf("CRITICAL INVARIANT A VIOLATION: %s resurrected to status %s after rollback to V3!", tombSKU, p.Status)
		}
	}

	// -------------------------------------------------------------------------
	// Deep Historical Rollback 3: Rollback to V1 AGAIN (Creating Version 8)
	// -------------------------------------------------------------------------
	v8Resp := rollbackTo(v1ID, "rollback-ops", "Rollback 3 -> V1 again")
	_ = v8Resp

	activeAfterV8 := getActiveProducts()
	if len(activeAfterV8) != 2 {
		t.Fatalf("ROLLBACK 3 VIOLATION: expected exactly 2 active items after second rollback to V1, got %d", len(activeAfterV8))
	}
	if pA, ok := activeAfterV8["SKU-A"]; !ok || pA.Price != 10.00 {
		t.Fatalf("SKU-A price should be 10.00, got %+v", pA)
	}
	if pB, ok := activeAfterV8["SKU-B"]; !ok || pB.Price != 20.00 {
		t.Fatalf("SKU-B price should be 20.00, got %+v", pB)
	}

	allAfterV8 := getAllProducts()
	for _, tombSKU := range []string{"SKU-TOMB1", "SKU-TOMB2", "SKU-C", "SKU-D", "SKU-E"} {
		p := allAfterV8[tombSKU]
		if p.Status != domain.StatusWithdrawn {
			t.Fatalf("CRITICAL INVARIANT A VIOLATION: %s resurrected to status %s on second rollback to V1!", tombSKU, p.Status)
		}
	}

	// -------------------------------------------------------------------------
	// Deep Historical Rollback 4: Rollback forward to V5 (Creating Version 9)
	// In V5, SKU-C, SKU-D, SKU-E were originally active. But they are now tombstones!
	// -------------------------------------------------------------------------
	v9Resp := rollbackTo(v5ID, "rollback-ops", "Rollback 4 -> V5")
	_ = v9Resp

	activeAfterV9 := getActiveProducts()
	if len(activeAfterV9) != 2 {
		t.Fatalf("ROLLBACK 4 VIOLATION: expected exactly 2 active items after rollback to V5, got %d: %+v", len(activeAfterV9), activeAfterV9)
	}

	allAfterV9 := getAllProducts()
	for _, tombSKU := range []string{"SKU-TOMB1", "SKU-TOMB2", "SKU-C", "SKU-D", "SKU-E"} {
		p := allAfterV9[tombSKU]
		if p.Status != domain.StatusWithdrawn {
			t.Fatalf("CRITICAL INVARIANT A VIOLATION: %s resurrected on rollback to V5!", tombSKU)
		}
	}

	// -------------------------------------------------------------------------
	// Deep Historical Rollback 5: Rollback to a ROLLBACK version! (V6) (Creating Version 10)
	// -------------------------------------------------------------------------
	v10Resp := rollbackTo(v6ID, "rollback-ops", "Rollback 5 -> V6 (Rollback of Rollback)")
	_ = v10Resp

	activeAfterV10 := getActiveProducts()
	if len(activeAfterV10) != 2 {
		t.Fatalf("ROLLBACK 5 VIOLATION: expected exactly 2 active items after rollback to V6, got %d", len(activeAfterV10))
	}
	allAfterV10 := getAllProducts()
	for _, tombSKU := range []string{"SKU-TOMB1", "SKU-TOMB2", "SKU-C", "SKU-D", "SKU-E"} {
		p := allAfterV10[tombSKU]
		if p.Status != domain.StatusWithdrawn {
			t.Fatalf("CRITICAL INVARIANT A VIOLATION: %s resurrected on rollback to V6!", tombSKU)
		}
	}
}

// =============================================================================
// CHALLENGE 3: CONCURRENT RACE: INTERLEAVED PUBLISH VS ROLLBACK ON SAME SHOP
// =============================================================================
// 10 concurrent goroutines: 5 constantly publishing patches, 5 constantly
// rolling back to random historical versions on the same tenant.
// Asserts zero data races (-race), zero panics, zero database deadlocks,
// and invariant preservation.
// =============================================================================
func TestChallenger_Concurrent_Publish_Rollback_Race(t *testing.T) {
	ts, _, _, _ := setupVersioningE2EServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	client := ts.Client()
	shopID := "shop-race-hardening"

	// Seed 2 initial versions
	t0 := time.Now().UTC()
	p1 := domain.CatalogPatch{
		Author:      "init",
		Description: "Seed V1",
		Items: []domain.PatchItem{
			{SKU: "RACE-A", Name: "Product A", Price: 10.0, Status: domain.StatusActive, Timestamp: &t0},
			{SKU: "RACE-TOMB", Name: "Tombstone", Price: 20.0, Status: domain.StatusWithdrawn, Timestamp: &t0},
		},
	}
	b1, _ := json.Marshal(p1)
	req1, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(b1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Shop-Id", shopID)
	r1, err := client.Do(req1)
	if err != nil || r1.StatusCode != http.StatusCreated {
		t.Fatalf("failed seeding V1: %v", err)
	}
	var pub1 api.PatchPublishResponse
	_ = json.NewDecoder(r1.Body).Decode(&pub1)
	r1.Body.Close()
	v1ID := pub1.Version.VersionID

	// Seed V2
	t1 := t0.Add(time.Minute)
	p2 := domain.CatalogPatch{
		Author:      "init",
		Description: "Seed V2",
		Items: []domain.PatchItem{
			{SKU: "RACE-A", Name: "Product A v2", Price: 15.0, Status: domain.StatusActive, Timestamp: &t1},
			{SKU: "RACE-B", Name: "Product B", Price: 30.0, Status: domain.StatusActive, Timestamp: &t1},
		},
	}
	b2, _ := json.Marshal(p2)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Shop-Id", shopID)
	r2, err := client.Do(req2)
	if err != nil || r2.StatusCode != http.StatusCreated {
		t.Fatalf("failed seeding V2: %v", err)
	}
	var pub2 api.PatchPublishResponse
	_ = json.NewDecoder(r2.Body).Decode(&pub2)
	r2.Body.Close()
	v2ID := pub2.Version.VersionID

	knownVersions := []string{v1ID, v2ID}
	var verMu sync.RWMutex

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	publishWorkers := 5
	rollbackWorkers := 5
	iterations := 4
	errChan := make(chan error, (publishWorkers+rollbackWorkers)*iterations*2)

	// Launch publish workers
	for w := 0; w < publishWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier

			for it := 0; it < iterations; it++ {
				now := time.Now().UTC()
				patch := domain.CatalogPatch{
					Author:      fmt.Sprintf("pub-worker-%d", workerID),
					Description: fmt.Sprintf("Concurrent publish W%d IT%d", workerID, it),
					Items: []domain.PatchItem{
						{
							SKU:       "RACE-A",
							Name:      fmt.Sprintf("Product A W%d IT%d", workerID, it),
							Price:     float64(10 + workerID*5 + it),
							Status:    domain.StatusActive,
							Timestamp: &now,
						},
					},
				}
				pBytes, _ := json.Marshal(patch)
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(pBytes))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Shop-Id", shopID)

				resp, err := client.Do(req)
				if err != nil {
					errChan <- fmt.Errorf("concurrent publish error: %w", err)
					return
				}
				if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusUnprocessableEntity {
					resp.Body.Close()
					errChan <- fmt.Errorf("concurrent publish expected 201 or 422, got %d", resp.StatusCode)
					return
				}
				if resp.StatusCode == http.StatusCreated {
					var pResp api.PatchPublishResponse
					_ = json.NewDecoder(resp.Body).Decode(&pResp)
					resp.Body.Close()

					verMu.Lock()
					knownVersions = append(knownVersions, pResp.Version.VersionID)
					verMu.Unlock()
				} else {
					resp.Body.Close()
				}
			}
		}(w)
	}

	// Launch rollback workers
	for w := 0; w < rollbackWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier

			for it := 0; it < iterations; it++ {
				verMu.RLock()
				targetVID := knownVersions[workerID%len(knownVersions)]
				verMu.RUnlock()

				rbBody, _ := json.Marshal(api.RollbackRequest{
					Author:      fmt.Sprintf("rb-worker-%d", workerID),
					Description: fmt.Sprintf("Concurrent rollback to %s by W%d", targetVID, workerID),
				})
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/versions/%s/rollback", ts.URL, targetVID), bytes.NewReader(rbBody))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Shop-Id", shopID)

				resp, err := client.Do(req)
				if err != nil {
					errChan <- fmt.Errorf("concurrent rollback error: %w", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					errChan <- fmt.Errorf("concurrent rollback expected 200, got %d", resp.StatusCode)
					return
				}
				var rbResp api.RollbackResponse
				_ = json.NewDecoder(resp.Body).Decode(&rbResp)
				resp.Body.Close()

				verMu.Lock()
				knownVersions = append(knownVersions, rbResp.Version.VersionID)
				verMu.Unlock()
			}
		}(w)
	}

	close(startBarrier)
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Fatalf("Concurrent race failure: %v", err)
	}

	// Post-race Invariant Check: Verify RACE-TOMB is STILL WITHDRAWN!
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/products?status=ACTIVE&limit=50", nil)
	req.Header.Set("X-Shop-Id", shopID)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed listing active products post-race: %v", err)
	}
	defer resp.Body.Close()

	var pList api.ProductListResponse
	_ = json.NewDecoder(resp.Body).Decode(&pList)
	for _, p := range pList.Products {
		if p.SKU == "RACE-TOMB" {
			t.Fatalf("CRITICAL RACE INVARIANT VIOLATION: RACE-TOMB was resurrected to ACTIVE during concurrent race!")
		}
	}
}
