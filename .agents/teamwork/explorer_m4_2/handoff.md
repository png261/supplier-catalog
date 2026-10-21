# Handoff Report: Embedded Static Assets & Frontend Dashboard Architecture

**Author**: `teamwork_preview_explorer` (explorer_m4_2)  
**Recipient**: Parent Orchestrator (`e6900bb2-6639-41ea-b0fb-48d815c2c1c8`)  
**Target Milestone**: Milestone 4 — Embedded Staff Web UI & Control Plane (`web/embed.go`, `web/static/*`)  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2`  

---

## 1. Observation

Direct examination of authoritative project requirements, interface contracts, and codebase state reveals the following:

1. **`ORIGINAL_REQUEST.md` (lines 28–31)**:
   > "R4. Minimalist Staff Interface & Status Reporting: Provide a clean, minimalist single-page dashboard embedded directly into the Go binary. Allow staff to start an update, view real-time completion progress, inspect a summary of changes (+Added, ~Updated, -Withdrawn, ↷Skipped), view issues needing attention, and resume interrupted updates."

2. **`PROJECT.md` (lines 22–25, 235–237)**:
   > - "4. Staff Dashboard & Control Plane (`pkg/api`, `pkg/server`, `web`): Zero-node embedded frontend via `//go:embed` serving minimalist single-page dashboard. Server-Sent Events (SSE) `/api/events` streaming real-time job progress and change metrics (+Added, ~Updated, -Withdrawn, ↷Skipped, ⚠Dead-Lettered). REST control APIs for triggering sync runs, resuming interrupted jobs, and inspecting paginated dead-letter records."
   > - Code layout specifies:
   >   ```
   >   web/
   >   ├── embed.go                 # Go embed.FS declaration
   >   └── static/                  # Minimalist single-page dashboard (index.html, app.js, style.css)
   >   ```

3. **`explorer_m4_2/DISPATCH.md` (lines 14–33)**:
   > - "1. Design embedded static asset mechanism: `web/embed.go` using standard Go 1.16+ `embed.FS` (`//go:embed static/*`). Serves static assets at `/` and `/static/` with fallback to `index.html`."
   > - "2. Design single-page dashboard architecture (`web/static/`): Zero node/npm dependencies: pure vanilla HTML5, CSS3, and ES6+ JavaScript."
   > - "3. Document full UI wireframes, DOM structure, CSS design tokens, and JavaScript event loops in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2/handoff.md`."

4. **`explorer_m4_1/handoff.md` (lines 119, 830–834, 938–950)**:
   > - Server integration: `cmd/server/main.go` parses CLI flag `--static-dir` (optional local disk directory overriding embedded assets for development hot-reloading).
   > - Main server router uses:
   >   ```go
   >   rootMux := http.NewServeMux()
   >   rootMux.Handle("/api/", apiHandler.Routes())
   >   rootMux.Handle("/", web.AssetHandler(opts.StaticDir))
   >   ```
   > - `http.Server.WriteTimeout` is explicitly set to `0` to support long-lived SSE connections.

5. **`spec_miner_m4_1/handoff.md` (lines 41–59, 103–285, 305–346)**:
   > - Standardized REST endpoint paths:
   >   - `POST /api/jobs`: Start job (`JobStartRequest`, returns `202 Accepted` with `SyncJobResponse`, or `409 Conflict` if job already running).
   >   - `POST /api/jobs/{id}/resume`: Resume job (`202 Accepted` or `409 Conflict`).
   >   - `GET /api/jobs`: List jobs (`JobListResponse` with `jobs`, `total`, `limit`, `offset`).
   >   - `GET /api/jobs/{id}`: Detailed inspection (`job`, `checkpoints`).
   >   - `GET /api/dead-letter`: Paginated DLQ (`issues`, `total`, `limit`, `offset`).
   >   - `GET /api/products`: Paginated catalog products (`products`, `total`, `limit`, `offset`, `search`, `status`).
   >   - `GET /api/events`: SSE endpoint streaming `job_started`, `progress`, `dead_letter`, `job_completed`, `job_interrupted`, and periodic `: ping\n\n` comments every 15 seconds.
   > - Error envelope: `{"error": "message", "code": "CODE"}`.

6. **`pkg/generator/config.go` (lines 28–43, 62–85)**:
   > - Parameter ratios: `update_ratio` (0.20), `duplicate_ratio` (0.05), `out_of_order_ratio` (0.10), `withdrawn_ratio` (0.05), `invalid_ratio` (0.05).
   > - Constraint: Sum of anomaly and update ratios must not exceed 1.0 (`totalRatios <= 1.0`), otherwise validation returns an error.

7. **Environment Observation**:
   > - Current directory tree contains no `web/` directory yet.
   > - Docker packaging constraint: Minimalist container images (Alpine/Distroless, $\le 25\text{MB}$) may lack `/etc/mime.types` files, meaning standard Go `http.FileServer` can fail to identify `.js` or `.css` MIME types unless explicitly registered.

---

## 2. Logic Chain

From the above observations, the design decisions are synthesized as follows:

1. **Embedded Assets Isolation (`web/embed.go`)**:
   - *Observation Reference*: Obs #2, Obs #3, Obs #4, Obs #7.
   - Go 1.16+ `embed.FS` via `//go:embed static/*` bundles all frontend files directly into the compiled Go binary with zero external file dependencies at runtime.
   - Using `fs.Sub(staticFS, "static")` strips the top-level directory prefix, allowing `http.FileServer` to map URLs like `/style.css` directly to `web/static/style.css`.
   - To support development hot-reloading without binary recompilation (Obs #4), `web.AssetHandler(staticDir string)` accepts an optional directory path. When `staticDir != ""`, it serves from `os.DirFS(staticDir)` / `http.Dir(staticDir)`; otherwise, it seamlessly serves from embedded `embed.FS`.
   - In minimal Alpine / Distroless containers (Obs #7), `mime.AddExtensionType` must be invoked during `init()` for `.js`, `.css`, `.html`, `.json`, `.svg`, and `.ico` to guarantee proper `Content-Type` headers.
   - Single-Page Application (SPA) routing requires that any non-API path (e.g., `/dashboard`, `/jobs`) that does not correspond to a physical static file serves `index.html` with status `200 OK` and `Cache-Control: no-cache, no-store, must-revalidate`.

2. **Zero-Dependency Single-Page Architecture (`web/static/`)**:
   - *Observation Reference*: Obs #1, Obs #2, Obs #3.
   - To strictly fulfill the zero-node/npm requirement, all frontend code is authored in standard HTML5, modern CSS3 (using CSS custom properties for theming, CSS Grid, and Flexbox), and vanilla ES6+ JavaScript (`EventSource`, `fetch`, async/await, DOM APIs). No external CDNs, bundlers, or third-party libraries are required.

3. **Live Metric Cards (5 Core Invariants)**:
   - *Observation Reference*: Obs #1, Obs #2, Obs #3, Obs #5.
   - To provide immediate visibility into the sync engine's invariants, five distinct cards are rendered:
     1. `+ Added` (Green / Emerald `#10b981`): New products inserted.
     2. `~ Updated` (Blue / Sky `#0ea5e9`): Existing products updated.
     3. `- Withdrawn` (Amber / Orange `#f59e0b`): Tombstoned items marked `WITHDRAWN`.
     4. `↷ Skipped` (Purple / Violet `#8b5cf6`): Out-of-order ($T_{new} \le T_{old}$), idempotent duplicates, or forbidden resurrects.
     5. `⚠ Dead-Lettered` (Red / Crimson `#ef4444`): Quarantined corrupt records.
   - Counter values update dynamically with smooth numerical animations on each incoming SSE `progress` event.

4. **SSE Event Stream & Connection Resilience**:
   - *Observation Reference*: Obs #4, Obs #5.
   - Browser initializes an `EventSource('/api/events')`.
   - Event listeners subscribe to:
     - `job_started`: Initializes progress telemetry and resets live counters.
     - `progress`: Increments metric cards, updates completion percentage, calculates velocity (records/sec) and ETA.
     - `dead_letter`: Updates DLQ indicator, inserts record into live DLQ table.
     - `job_completed`: Sets progress bar to 100%, transitions status badge to `COMPLETED`, triggers catalog refresh.
     - `job_interrupted`: Transitions status badge to `INTERRUPTED`, updates interrupted job selector.
   - Reconnection & Keepalive: If connection drops, `EventSource` automatically reconnects. A 35-second client-side watchdog detects missed 15s keepalive pings (`: ping\n\n`) and triggers proactive reconnection.

5. **Staff Control Plane & Resumability**:
   - *Observation Reference*: Obs #1, Obs #5, Obs #6.
   - A multi-tab control card allows staff to:
     - **Tab 1 (Generator)**: Select presets ("Default", "Clean", "Stress Test") or configure custom parameters (Seed, Batches, Batch Size, and Anomaly Sliders). A client-side validator calculates `Duplicate% + OutOfOrder% + Withdrawn% + Invalid%` and disables submission with visual warning if $> 100\%$.
     - **Tab 2 (Manual Batch)**: Paste raw JSON records to test custom supplier payloads.
     - **Tab 3 (Resume Interrupted)**: Populates a dropdown from `GET /api/jobs?status=INTERRUPTED` and triggers `POST /api/jobs/{id}/resume`.
   - When a job is active, buttons are disabled and display a running spinner. If a conflict occurs (`409 Conflict`), a floating toast alert informs the operator.

6. **Dead-Letter Queue Inspector & Raw Payload Modal**:
   - *Observation Reference*: Obs #1, Obs #2, Obs #5.
   - A paginated table displays quarantined issues (`GET /api/dead-letter`).
   - Clicking "View Payload" opens a modal displaying the formatted JSON payload with syntax highlighting, failure reason explanation, and a "Copy JSON" button.

7. **Catalog Products Explorer**:
   - *Observation Reference*: Obs #5.
   - A paginated, searchable product table (`GET /api/products`) with 250ms debounced search on SKU/Name and status filter (`ALL`, `ACTIVE`, `WITHDRAWN`), formatted currency, and UTC timestamps.

---

## 3. Caveats

1. **Browser Compatibility**: The frontend uses modern standard ES6+ (`EventSource`, `fetch`, CSS variables, flexbox/grid). It is fully compatible with modern Chrome, Firefox, Safari, Edge, and mobile browsers. Legacy browsers without `EventSource` (e.g. Internet Explorer 11) are not supported.
2. **Reverse Proxy Buffering**: When deployed behind reverse proxies (Nginx, Cloudflare), SSE streaming requires `X-Accel-Buffering: no` and proxy buffering disabled. Explorer M4.1 has included this header in the SSE endpoint handshake.
3. **Local Storage**: Dark/Light theme preferences are saved in `localStorage`. If `localStorage` is disabled in the browser, the theme defaults to the system preference (`prefers-color-scheme`).

---

## 4. Conclusion & Technical Design

### 4.1 UI Architecture Wireframe & Layout

```
+---------------------------------------------------------------------------------------------------------+
| [⚡ CODERPUSH CATALOG SYNC ENGINE]       [DB: SQLite WAL (Active)]  [● Live SSE Connected]  [Theme ☀/🌙]  |
| Active Job: job-1771678800000 | RUNNING | Batch 3/10 (30%) | Runtime: 00:01:24                          |
+---------------------------------------------------------------------------------------------------------+
| CONTROL PLANE                                                                                           |
| +-----------------------------------------------------------------------------------------------------+ |
| | [ Tab: Mock Generator ]   [ Tab: Manual JSON Payload ]   [ Tab: Resume Interrupted Job (2) ]        | |
| +-----------------------------------------------------------------------------------------------------+ |
| | Presets: [ Default (10x100) ]  [ Clean Catalog (0% Anom) ]  [ DLQ Stress Test (High Corrupt) ]      | |
| | Seed: [ 42        ] [🎲 Random]   Batches: [ 10 ]   Batch Size: [ 100 ]                             | |
| | Sliders: Duplicate: 5% | Out-of-Order: 10% | Withdrawn: 5% | Invalid (DLQ): 5%                      | |
| | Validation Status: ✓ Ratios valid (25.0% <= 100%)                                                   | |
| | [ ▶ Start Sync Job ]   [ ↺ Reset Defaults ]                                                         | |
| +-----------------------------------------------------------------------------------------------------+ |
+---------------------------------------------------------------------------------------------------------+
| LIVE METRIC CARDS (5 Core Invariants)                                                                   |
| +-----------------+ +-----------------+ +-----------------+ +-----------------+ +-----------------+ |
| |   + ADDED       | |   ~ UPDATED     | |   - WITHDRAWN   | |   ↷ SKIPPED     | | ⚠ DEAD-LETTER   | |
| |   800           | |   100           | |   50            | |   30            | |   20            | |
| |   +80 this batch| |   +10 this batch| |   +5 this batch | |   +3 this batch | |   +2 this batch | |
| |   (Green)       | |   (Blue)        | |   (Amber)       | |   (Purple)      | |   (Red)         | |
| +-----------------+ +-----------------+ +-----------------+ +-----------------+ +-----------------+ |
+---------------------------------------------------------------------------------------------------------+
| REAL-TIME PROGRESS & TELEMETRY                                                                          |
| Progress: [====================================>                       ] 30.0% (Batch 3 of 10)          |
| Throughput: 1,240 records/sec  |  ETA: 00:07s  |  Memory: In-Memory / WAL                                |
| +-----------------------------------------------------------------------------------------------------+ |
| | Live Event Audit Stream                                                [Auto-scroll: ON] [Clear]     | |
| | [13:00:01.120] [JOB_STARTED] Job job-101 started: 10 batches, size 100                               | |
| | [13:00:01.350] [PROGRESS]    Batch 0 committed: +80 added, ~10 updated, -5 withdrawn, ↷3 skipped     | |
| | [13:00:01.352] [DEAD_LETTER] Quarantined record SKU='BAD-PRICE-01': price cannot be negative        | |
| | [13:00:01.580] [PROGRESS]    Batch 1 committed: +80 added, ~10 updated, -5 withdrawn, ↷3 skipped     | |
| +-----------------------------------------------------------------------------------------------------+ |
+---------------------------------------------------------------------------------------------------------+
| DATA EXPLORERS                                                                                          |
| +-----------------------------------------------------------------------------------------------------+ |
| | [ Tab: ⚠ Dead-Letter Queue (20) ]       [ Tab: 📦 Catalog Products (1,450) ]     [ Tab: Job History ]| |
| +-----------------------------------------------------------------------------------------------------+ |
| | Filter Job: [ All Jobs ▼ ]   Search: [                   ]   [ ↻ Refresh ]                           | |
| | +-----------------------+---------+--------+--------------------------+-----------------+-----------+ | |
| | | Issue ID              | Job ID  | Batch# | SKU                      | Defect Reason   | Created At|Act| | |
| | +-----------------------+---------+--------+--------------------------+-----------------+-----------+ | |
| | | dl-101-0-12-8801      | job-101 | 0      | BAD-PRICE-01             | negative price  | 13:00:01  |👁 | |
| | | dl-101-1-04-8802      | job-101 | 1      | [EMPTY SKU]              | empty SKU       | 13:00:02  |👁 | |
| | +-----------------------+---------+--------+--------------------------+-----------------+-----------+ | |
| | Showing 1-10 of 20 issues                                    [ < Previous ] Page 1 of 2 [ Next > ]    | |
| +-----------------------------------------------------------------------------------------------------+ |
+---------------------------------------------------------------------------------------------------------+
| MODAL: RAW PAYLOAD INSPECTOR                                                                            |
| +-----------------------------------------------------------------------------------------------------+ |
| | Quarantined Record Payload                                                                      [X] | |
| | Defect: price cannot be negative | Issue ID: dl-101-0-12-8801                                       | |
| | ```json                                                                                             | |
| | {                                                                                                   | |
| |   "sku": "BAD-PRICE-01",                                                                            | |
| |   "name": "Faulty Mechanical Switch",                                                               | |
| |   "price": -12.50,                                                                                  | |
| |   "status": "ACTIVE",                                                                               | |
| |   "timestamp": "2026-01-01T00:00:00Z"                                                               | |
| | }                                                                                                   | |
| | ```                                                                                                 | |
| | [ 📋 Copy JSON Payload ]                                                            [ Close ]       | |
| +-----------------------------------------------------------------------------------------------------+ |
+---------------------------------------------------------------------------------------------------------+
```

---

### 4.2 Embedded Asset Handler Implementation (`web/embed.go`)

```go
package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

func init() {
	// Register essential MIME types explicitly so that embedded assets
	// serve with correct Content-Type headers even in minimal container environments (Alpine/Distroless).
	_ = mime.AddExtensionType(".html", "text/html; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".json", "application/json; charset=utf-8")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".png", "image/png")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
}

// AssetHandler returns an http.Handler that serves embedded static dashboard files.
// If diskDir is non-empty, it serves files directly from the specified disk directory (enabling hot-reloading).
// If diskDir is empty, it serves files from the binary's embedded embed.FS.
// The handler serves "/" with index.html, supports paths under "/static/", and falls back to index.html for SPA routes.
func AssetHandler(diskDir string) http.Handler {
	var fileSystem http.FileSystem
	var fsRoot fs.FS

	if diskDir != "" {
		fileSystem = http.Dir(diskDir)
		fsRoot = os.DirFS(diskDir)
	} else {
		sub, err := fs.Sub(staticFS, "static")
		if err != nil {
			fsRoot = staticFS
			fileSystem = http.FS(staticFS)
		} else {
			fsRoot = sub
			fileSystem = http.FS(sub)
		}
	}

	fileServer := http.FileServer(fileSystem)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean and normalize requested path
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
		}
		upath = path.Clean(upath)

		// Strip /static prefix if present
		trimmedPath := strings.TrimPrefix(upath, "/static")
		if trimmedPath == "" {
			trimmedPath = "/"
		}

		// Root request -> serve index.html with no-cache headers to ensure immediate UI updates
		if trimmedPath == "/" || trimmedPath == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			r.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r)
			return
		}

		// Check if physical file exists in the asset filesystem
		cleanRel := strings.TrimPrefix(trimmedPath, "/")
		if fileExists(fsRoot, cleanRel) {
			r.URL.Path = trimmedPath
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA Fallback: If not an API route and not a physical file, serve index.html
		if !strings.HasPrefix(upath, "/api/") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			r.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})
}

// fileExists checks whether a physical file exists and is not a directory.
func fileExists(f fs.FS, name string) bool {
	if f == nil || name == "" {
		return false
	}
	stat, err := fs.Stat(f, name)
	if err != nil || stat.IsDir() {
		return false
	}
	return true
}
```

---

### 4.3 Embedded Asset Automated Tests (`web/embed_test.go`)

```go
package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetHandler_Embedded(t *testing.T) {
	handler := AssetHandler("")

	t.Run("Serve index.html at root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected text/html Content-Type, got %s", ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Coderpush Catalog Sync Engine") {
			t.Errorf("expected dashboard title in index.html, got: %s", body)
		}
	})

	t.Run("Serve style.css directly and via /static prefix", func(t *testing.T) {
		paths := []string{"/style.css", "/static/style.css"}
		for _, p := range paths {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("path %s: expected 200 OK, got %d", p, rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "text/css") {
				t.Errorf("path %s: expected text/css Content-Type, got %s", p, ct)
			}
		}
	})

	t.Run("Serve app.js directly and via /static prefix", func(t *testing.T) {
		paths := []string{"/app.js", "/static/app.js"}
		for _, p := range paths {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("path %s: expected 200 OK, got %d", p, rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "javascript") {
				t.Errorf("path %s: expected javascript Content-Type, got %s", p, ct)
			}
		}
	})

	t.Run("SPA fallback for non-existing route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/unknown-page", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK fallback, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Coderpush Catalog Sync Engine") {
			t.Errorf("expected fallback to index.html")
		}
	})

	t.Run("404 for unhandled API route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/unhandled", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for /api/ path, got %d", rec.Code)
		}
	})
}
```

---

### 4.4 Dashboard HTML Structure (`web/static/index.html`)

```html
<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Coderpush Catalog Sync Engine</title>
  <link rel="stylesheet" href="/style.css">
  <link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>⚡</text></svg>">
</head>
<body>
  <!-- Header & Status Bar -->
  <header class="app-header">
    <div class="header-brand">
      <span class="brand-icon">⚡</span>
      <h1 class="brand-title">Coderpush Catalog Sync Engine</h1>
      <span class="badge badge-db">SQLite WAL Active</span>
    </div>
    <div class="header-status">
      <div id="active-job-badge" class="badge badge-job status-idle">
        <span class="status-indicator"></span>
        <span id="active-job-text">IDLE — No active sync</span>
      </div>
      <div id="sse-status" class="badge badge-sse sse-connecting">
        <span class="sse-dot"></span>
        <span id="sse-status-text">Connecting SSE...</span>
      </div>
      <button id="theme-toggle" class="btn btn-icon" title="Toggle Dark/Light Mode" aria-label="Toggle theme">
        <span id="theme-icon">🌙</span>
      </button>
    </div>
  </header>

  <main class="dashboard-container">
    <!-- Section 1: Control Plane -->
    <section class="card control-card">
      <div class="card-header">
        <div class="tab-group" role="tablist">
          <button class="tab-btn active" data-tab="tab-generator" role="tab" aria-selected="true">⚡ Mock Generator</button>
          <button class="tab-btn" data-tab="tab-resume" role="tab" aria-selected="false">↺ Resume Interrupted</button>
          <button class="tab-btn" data-tab="tab-manual" role="tab" aria-selected="false">📄 Custom JSON Batch</button>
        </div>
      </div>

      <!-- Tab 1: Mock Generator -->
      <div id="tab-generator" class="tab-content active" role="tabpanel">
        <div class="preset-bar">
          <span class="preset-label">Presets:</span>
          <button class="btn btn-sm btn-outline preset-btn" data-preset="default">Default (10x100)</button>
          <button class="btn btn-sm btn-outline preset-btn" data-preset="clean">Clean (0% Anomaly)</button>
          <button class="btn btn-sm btn-outline preset-btn" data-preset="stress">Chaos DLQ Stress</button>
          <button class="btn btn-sm btn-outline preset-btn" data-preset="large">Large Run (50x200)</button>
        </div>

        <form id="generator-form" class="form-grid">
          <div class="form-group">
            <label for="input-seed">Seed (PRNG):</label>
            <div class="input-inline">
              <input type="number" id="input-seed" name="seed" value="42" min="0" required>
              <button type="button" id="btn-random-seed" class="btn btn-sm btn-secondary" title="Randomize seed">🎲</button>
            </div>
          </div>
          <div class="form-group">
            <label for="input-batches">Total Batches:</label>
            <input type="number" id="input-batches" name="total_batches" value="10" min="1" max="1000" required>
          </div>
          <div class="form-group">
            <label for="input-batch-size">Batch Size:</label>
            <input type="number" id="input-batch-size" name="batch_size" value="100" min="10" max="5000" required>
          </div>
          <div class="form-group">
            <label for="input-update-ratio">Update Ratio: <span id="val-update-ratio">20%</span></label>
            <input type="range" id="input-update-ratio" min="0" max="100" value="20" step="1">
          </div>
          <div class="form-group">
            <label for="input-dup-ratio">Duplicate Ratio: <span id="val-dup-ratio">5%</span></label>
            <input type="range" id="input-dup-ratio" min="0" max="100" value="5" step="1">
          </div>
          <div class="form-group">
            <label for="input-ooo-ratio">Out-of-Order Ratio: <span id="val-ooo-ratio">10%</span></label>
            <input type="range" id="input-ooo-ratio" min="0" max="100" value="10" step="1">
          </div>
          <div class="form-group">
            <label for="input-withdrawn-ratio">Withdrawn Ratio: <span id="val-withdrawn-ratio">5%</span></label>
            <input type="range" id="input-withdrawn-ratio" min="0" max="100" value="5" step="1">
          </div>
          <div class="form-group">
            <label for="input-invalid-ratio">Invalid Ratio (DLQ): <span id="val-invalid-ratio">5%</span></label>
            <input type="range" id="input-invalid-ratio" min="0" max="100" value="5" step="1">
          </div>

          <div class="form-footer">
            <div id="ratio-validator" class="ratio-validator valid">
              Sum of Ratios: <span id="total-ratio-val">45%</span> (Max: 100%)
            </div>
            <div class="action-buttons">
              <button type="submit" id="btn-start-job" class="btn btn-primary">
                <span class="btn-spinner hidden"></span>
                <span>▶ Start Synchronization</span>
              </button>
              <button type="button" id="btn-reset-form" class="btn btn-secondary">↺ Reset</button>
            </div>
          </div>
        </form>
      </div>

      <!-- Tab 2: Resume Interrupted -->
      <div id="tab-resume" class="tab-content" role="tabpanel">
        <div class="resume-container">
          <p class="section-desc">Select an interrupted synchronization job to resume directly from its last uncommitted batch checkpoint.</p>
          <div class="resume-controls">
            <div class="form-group resume-select-group">
              <label for="select-interrupted-job">Interrupted Jobs:</label>
              <select id="select-interrupted-job" class="form-select">
                <option value="">Loading interrupted jobs...</option>
              </select>
            </div>
            <button type="button" id="btn-refresh-interrupted" class="btn btn-secondary" title="Refresh job list">↻</button>
            <button type="button" id="btn-resume-job" class="btn btn-primary" disabled>
              <span class="btn-spinner hidden"></span>
              <span>↺ Resume Job</span>
            </button>
          </div>
          <div id="resume-job-details" class="job-meta-preview hidden">
            <!-- Rendered by JS -->
          </div>
        </div>
      </div>

      <!-- Tab 3: Custom JSON Batch -->
      <div id="tab-manual" class="tab-content" role="tabpanel">
        <div class="manual-container">
          <p class="section-desc">Submit raw supplier JSON batch records to test custom catalog updates directly.</p>
          <div class="textarea-actions">
            <button type="button" id="btn-load-sample-json" class="btn btn-sm btn-outline">Load Sample JSON</button>
            <button type="button" id="btn-clear-json" class="btn btn-sm btn-outline">Clear</button>
          </div>
          <textarea id="manual-json-input" class="code-editor" placeholder="[&#10;  {&#10;    &quot;sku&quot;: &quot;PROD-101&quot;,&#10;    &quot;name&quot;: &quot;Sample Widget&quot;,&#10;    &quot;price&quot;: 29.99,&#10;    &quot;status&quot;: &quot;ACTIVE&quot;,&#10;    &quot;timestamp&quot;: &quot;2026-10-21T12:00:00Z&quot;&#10;  }&#10;]"></textarea>
          <div class="form-footer">
            <span id="json-parse-status" class="status-msg"></span>
            <button type="button" id="btn-submit-manual" class="btn btn-primary">Submit Batch</button>
          </div>
        </div>
      </div>
    </section>

    <!-- Section 2: Live Invariant Metric Cards -->
    <section class="metrics-grid">
      <!-- Added -->
      <div class="metric-card card-added">
        <div class="metric-icon">+</div>
        <div class="metric-body">
          <div class="metric-label">Added</div>
          <div id="metric-added" class="metric-value">0</div>
          <div class="metric-sub"><span id="delta-added">+0</span> in last batch</div>
        </div>
      </div>

      <!-- Updated -->
      <div class="metric-card card-updated">
        <div class="metric-icon">~</div>
        <div class="metric-body">
          <div class="metric-label">Updated</div>
          <div id="metric-updated" class="metric-value">0</div>
          <div class="metric-sub"><span id="delta-updated">+0</span> in last batch</div>
        </div>
      </div>

      <!-- Withdrawn -->
      <div class="metric-card card-withdrawn">
        <div class="metric-icon">-</div>
        <div class="metric-body">
          <div class="metric-label">Withdrawn</div>
          <div id="metric-withdrawn" class="metric-value">0</div>
          <div class="metric-sub"><span id="delta-withdrawn">+0</span> in last batch</div>
        </div>
      </div>

      <!-- Skipped -->
      <div class="metric-card card-skipped">
        <div class="metric-icon">↷</div>
        <div class="metric-body">
          <div class="metric-label">Skipped</div>
          <div id="metric-skipped" class="metric-value">0</div>
          <div class="metric-sub"><span id="delta-skipped">+0</span> in last batch</div>
        </div>
      </div>

      <!-- Dead-Lettered -->
      <div class="metric-card card-deadletter">
        <div class="metric-icon">⚠</div>
        <div class="metric-body">
          <div class="metric-label">Dead-Lettered</div>
          <div id="metric-deadletter" class="metric-value">0</div>
          <div class="metric-sub"><span id="delta-deadletter">+0</span> in last batch</div>
        </div>
      </div>
    </section>

    <!-- Section 3: Progress & Live Telemetry Stream -->
    <section class="card progress-card">
      <div class="progress-header">
        <div class="progress-title">
          <span>Batch Progress:</span>
          <strong id="batch-progress-text">0 / 0 Batches (0.0%)</strong>
        </div>
        <div class="progress-telemetry">
          <span class="telemetry-item">Speed: <strong id="telemetry-speed">0 rec/s</strong></span>
          <span class="telemetry-item">ETA: <strong id="telemetry-eta">—</strong></span>
          <span class="telemetry-item">Elapsed: <strong id="telemetry-runtime">00:00</strong></span>
        </div>
      </div>

      <div class="progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="0">
        <div id="progress-bar-fill" class="progress-fill" style="width: 0%"></div>
      </div>

      <!-- Collapsible Live Event Console -->
      <div class="event-console">
        <div class="console-header">
          <div class="console-title">
            <span class="pulse-indicator"></span>
            <span>Live Audit Event Stream (SSE)</span>
          </div>
          <div class="console-actions">
            <label class="checkbox-inline">
              <input type="checkbox" id="check-autoscroll" checked>
              <span>Auto-scroll</span>
            </label>
            <button id="btn-clear-console" class="btn btn-xs btn-outline">Clear Log</button>
          </div>
        </div>
        <div id="event-log-container" class="event-log-list">
          <div class="log-entry log-system">[00:00:00] SSE Listener initialized. Waiting for events...</div>
        </div>
      </div>
    </section>

    <!-- Section 4: Data Explorers (DLQ, Products, History) -->
    <section class="card explorer-card">
      <div class="card-header">
        <div class="tab-group" role="tablist">
          <button class="tab-btn active" data-tab="tab-dlq" role="tab">⚠ Dead-Letter Queue (<span id="count-dlq-badge">0</span>)</button>
          <button class="tab-btn" data-tab="tab-catalog" role="tab">📦 Product Catalog (<span id="count-catalog-badge">0</span>)</button>
          <button class="tab-btn" data-tab="tab-history" role="tab">📋 Sync History</button>
        </div>
      </div>

      <!-- Explorer Tab 1: Dead Letter Queue -->
      <div id="tab-dlq" class="tab-content active" role="tabpanel">
        <div class="table-toolbar">
          <div class="filter-group">
            <label for="filter-dlq-job">Filter Job:</label>
            <input type="text" id="filter-dlq-job" class="form-input form-sm" placeholder="Enter Job ID or leave empty for all">
          </div>
          <button id="btn-refresh-dlq" class="btn btn-sm btn-secondary">↻ Refresh DLQ</button>
        </div>

        <div class="table-responsive">
          <table class="data-table">
            <thead>
              <tr>
                <th>Issue ID</th>
                <th>Job ID</th>
                <th>Batch</th>
                <th>SKU</th>
                <th>Defect Reason</th>
                <th>Quarantined At</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody id="dlq-table-body">
              <tr><td colspan="7" class="table-empty">No corrupt records in Dead-Letter Queue.</td></tr>
            </tbody>
          </table>
        </div>

        <div class="pagination-bar">
          <span id="dlq-pagination-info" class="pagination-info">Showing 0-0 of 0</span>
          <div class="pagination-buttons">
            <button id="btn-dlq-prev" class="btn btn-sm btn-outline" disabled>Previous</button>
            <span id="dlq-page-num" class="page-indicator">Page 1</span>
            <button id="btn-dlq-next" class="btn btn-sm btn-outline" disabled>Next</button>
          </div>
        </div>
      </div>

      <!-- Explorer Tab 2: Catalog Products -->
      <div id="tab-catalog" class="tab-content" role="tabpanel">
        <div class="table-toolbar">
          <div class="filter-group">
            <input type="text" id="filter-product-search" class="form-input form-sm" placeholder="Search SKU or Product Name...">
          </div>
          <div class="filter-group">
            <select id="filter-product-status" class="form-select form-sm">
              <option value="">All Statuses</option>
              <option value="ACTIVE">ACTIVE Only</option>
              <option value="WITHDRAWN">WITHDRAWN Only</option>
            </select>
          </div>
          <button id="btn-refresh-catalog" class="btn btn-sm btn-secondary">↻ Refresh Catalog</button>
        </div>

        <div class="table-responsive">
          <table class="data-table">
            <thead>
              <tr>
                <th>SKU</th>
                <th>Product Name</th>
                <th>Price</th>
                <th>Status</th>
                <th>Supplier Timestamp</th>
                <th>Local Updated At</th>
              </tr>
            </thead>
            <tbody id="catalog-table-body">
              <tr><td colspan="6" class="table-empty">Loading product catalog...</td></tr>
            </tbody>
          </table>
        </div>

        <div class="pagination-bar">
          <span id="catalog-pagination-info" class="pagination-info">Showing 0-0 of 0</span>
          <div class="pagination-buttons">
            <button id="btn-catalog-prev" class="btn btn-sm btn-outline" disabled>Previous</button>
            <span id="catalog-page-num" class="page-indicator">Page 1</span>
            <button id="btn-catalog-next" class="btn btn-sm btn-outline" disabled>Next</button>
          </div>
        </div>
      </div>

      <!-- Explorer Tab 3: Sync History -->
      <div id="tab-history" class="tab-content" role="tabpanel">
        <div class="table-toolbar">
          <button id="btn-refresh-history" class="btn btn-sm btn-secondary">↻ Refresh History</button>
        </div>
        <div class="table-responsive">
          <table class="data-table">
            <thead>
              <tr>
                <th>Job ID</th>
                <th>Status</th>
                <th>Progress</th>
                <th>Added</th>
                <th>Updated</th>
                <th>Withdrawn</th>
                <th>Skipped</th>
                <th>Dead-Letter</th>
                <th>Created At</th>
              </tr>
            </thead>
            <tbody id="history-table-body">
              <tr><td colspan="9" class="table-empty">Loading sync history...</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </section>
  </main>

  <!-- Modal: Raw Payload Inspector -->
  <div id="payload-modal" class="modal-backdrop hidden" role="dialog" aria-modal="true" aria-labelledby="modal-title">
    <div class="modal-dialog">
      <div class="modal-header">
        <h3 id="modal-title" class="modal-title">Quarantined Record Payload</h3>
        <button id="btn-close-modal" class="btn btn-icon modal-close" aria-label="Close modal">✕</button>
      </div>
      <div class="modal-body">
        <div class="modal-meta">
          <div><strong>Issue ID:</strong> <span id="modal-issue-id" class="code-tag">—</span></div>
          <div><strong>Defect Reason:</strong> <span id="modal-defect-reason" class="defect-badge">—</span></div>
        </div>
        <div class="code-block-wrapper">
          <pre id="modal-payload-code" class="code-block"></pre>
        </div>
      </div>
      <div class="modal-footer">
        <button id="btn-copy-payload" class="btn btn-outline">📋 Copy JSON</button>
        <button id="btn-dismiss-modal" class="btn btn-secondary">Close</button>
      </div>
    </div>
  </div>

  <!-- Toast Notification Container -->
  <div id="toast-container" class="toast-container" aria-live="polite"></div>

  <!-- Application Script -->
  <script src="/app.js"></script>
</body>
</html>
```

---

### 4.5 Engineering Design Tokens & Responsive CSS (`web/static/style.css`)

```css
/* ==========================================================================
   CSS Design Tokens (Engineering Dark & Light Modes)
   ========================================================================== */
:root {
  /* Dark Theme (Default) */
  --bg-base: #090d16;
  --bg-surface: #111827;
  --bg-card: #161f30;
  --bg-card-hover: #1e293e;
  --bg-input: #0d131f;
  
  --border-subtle: #1f2a3f;
  --border-strong: #2e3e5c;
  --border-focus: #3b82f6;

  --text-primary: #f8fafc;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --text-inverse: #090d16;

  /* Semantic Invariant Colors */
  --color-added: #10b981;
  --color-added-bg: rgba(16, 185, 129, 0.12);
  --color-updated: #0ea5e9;
  --color-updated-bg: rgba(14, 165, 233, 0.12);
  --color-withdrawn: #f59e0b;
  --color-withdrawn-bg: rgba(245, 158, 11, 0.12);
  --color-skipped: #8b5cf6;
  --color-skipped-bg: rgba(139, 92, 246, 0.12);
  --color-deadletter: #ef4444;
  --color-deadletter-bg: rgba(239, 68, 68, 0.12);

  /* Functional Status */
  --color-idle: #64748b;
  --color-running: #0ea5e9;
  --color-completed: #10b981;
  --color-interrupted: #f59e0b;
  --color-failed: #ef4444;

  /* Typography & Radius */
  --font-sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  --radius-sm: 4px;
  --radius-md: 6px;
  --radius-lg: 10px;
  --shadow-sm: 0 1px 2px 0 rgba(0, 0, 0, 0.25);
  --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.35);
  --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.5);
  --transition-fast: 150ms cubic-bezier(0.4, 0, 0.2, 1);
}

[data-theme="light"] {
  --bg-base: #f8fafc;
  --bg-surface: #ffffff;
  --bg-card: #ffffff;
  --bg-card-hover: #f1f5f9;
  --bg-input: #f8fafc;

  --border-subtle: #e2e8f0;
  --border-strong: #cbd5e1;
  --border-focus: #2563eb;

  --text-primary: #0f172a;
  --text-secondary: #475569;
  --text-muted: #94a3b8;
  --text-inverse: #ffffff;

  --color-added-bg: rgba(16, 185, 129, 0.08);
  --color-updated-bg: rgba(14, 165, 233, 0.08);
  --color-withdrawn-bg: rgba(245, 158, 11, 0.08);
  --color-skipped-bg: rgba(139, 92, 246, 0.08);
  --color-deadletter-bg: rgba(239, 68, 68, 0.08);

  --shadow-sm: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
  --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.08);
  --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.12);
}

/* ==========================================================================
   Base & Layout
   ========================================================================== */
* {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

body {
  font-family: var(--font-sans);
  background-color: var(--bg-base);
  color: var(--text-primary);
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
  min-height: 100vh;
}

.dashboard-container {
  max-width: 1400px;
  margin: 0 auto;
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

/* ==========================================================================
   Header & Status Bar
   ========================================================================== */
.app-header {
  background-color: var(--bg-surface);
  border-bottom: 1px solid var(--border-subtle);
  padding: 0.85rem 1.5rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  position: sticky;
  top: 0;
  z-index: 50;
  box-shadow: var(--shadow-sm);
}

.header-brand {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.brand-icon {
  font-size: 1.5rem;
}

.brand-title {
  font-size: 1.15rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.header-status {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

/* Badges */
.badge {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.3rem 0.65rem;
  font-size: 0.75rem;
  font-weight: 600;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}

.badge-db {
  background-color: rgba(14, 165, 233, 0.1);
  color: var(--color-updated);
  border-color: rgba(14, 165, 233, 0.3);
}

.badge-job {
  font-family: var(--font-mono);
  background-color: var(--bg-card);
}

.status-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background-color: var(--color-idle);
}

.status-idle .status-indicator { background-color: var(--color-idle); }
.status-running .status-indicator { background-color: var(--color-running); box-shadow: 0 0 8px var(--color-running); animation: pulse 1.5s infinite; }
.status-completed .status-indicator { background-color: var(--color-completed); }
.status-interrupted .status-indicator { background-color: var(--color-interrupted); }
.status-failed .status-indicator { background-color: var(--color-failed); }

.badge-sse {
  background-color: var(--bg-card);
}

.sse-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.sse-connected .sse-dot { background-color: var(--color-added); box-shadow: 0 0 6px var(--color-added); }
.sse-connecting .sse-dot { background-color: var(--color-withdrawn); animation: pulse 1s infinite; }
.sse-disconnected .sse-dot { background-color: var(--color-deadletter); }

/* ==========================================================================
   Cards & Tabs
   ========================================================================== */
.card {
  background-color: var(--bg-card);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
}

.card-header {
  border-bottom: 1px solid var(--border-subtle);
  padding: 0.75rem 1.25rem;
  background-color: rgba(0, 0, 0, 0.05);
}

.tab-group {
  display: flex;
  gap: 0.5rem;
}

.tab-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-weight: 600;
  font-size: 0.85rem;
  padding: 0.45rem 0.9rem;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.tab-btn:hover {
  color: var(--text-primary);
  background-color: var(--bg-card-hover);
}

.tab-btn.active {
  color: var(--text-primary);
  background-color: var(--bg-surface);
  border: 1px solid var(--border-strong);
}

.tab-content {
  display: none;
  padding: 1.25rem;
}

.tab-content.active {
  display: block;
}

/* ==========================================================================
   Control Plane Form
   ========================================================================== */
.preset-bar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1.25rem;
  flex-wrap: wrap;
}

.preset-label {
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--text-muted);
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 1.25rem;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.form-group label {
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--text-secondary);
  display: flex;
  justify-content: space-between;
}

.form-group input[type="number"],
.form-input,
.form-select {
  background-color: var(--bg-input);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  color: var(--text-primary);
  font-size: 0.85rem;
  padding: 0.5rem 0.75rem;
  outline: none;
  transition: border-color var(--transition-fast);
}

.form-group input[type="number"]:focus,
.form-input:focus,
.form-select:focus {
  border-color: var(--border-focus);
}

.input-inline {
  display: flex;
  gap: 0.5rem;
}

.input-inline input {
  flex: 1;
}

input[type="range"] {
  accent-color: var(--border-focus);
  cursor: pointer;
}

.form-footer {
  grid-column: 1 / -1;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 1rem;
  border-top: 1px solid var(--border-subtle);
  flex-wrap: wrap;
  gap: 1rem;
}

.ratio-validator {
  font-size: 0.85rem;
  font-weight: 600;
}

.ratio-validator.valid {
  color: var(--color-added);
}

.ratio-validator.invalid {
  color: var(--color-deadletter);
}

.action-buttons {
  display: flex;
  gap: 0.75rem;
}

/* Resume Section */
.resume-container,
.manual-container {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.section-desc {
  font-size: 0.85rem;
  color: var(--text-secondary);
}

.resume-controls {
  display: flex;
  gap: 0.75rem;
  align-items: flex-end;
  max-width: 700px;
}

.resume-select-group {
  flex: 1;
}

/* ==========================================================================
   Buttons
   ========================================================================== */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  font-size: 0.85rem;
  font-weight: 600;
  padding: 0.5rem 1rem;
  border-radius: var(--radius-sm);
  border: 1px solid transparent;
  cursor: pointer;
  transition: all var(--transition-fast);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-primary {
  background-color: #2563eb;
  color: #ffffff;
}

.btn-primary:hover:not(:disabled) {
  background-color: #1d4ed8;
}

.btn-secondary {
  background-color: var(--bg-surface);
  color: var(--text-primary);
  border-color: var(--border-strong);
}

.btn-secondary:hover:not(:disabled) {
  background-color: var(--bg-card-hover);
}

.btn-outline {
  background: none;
  color: var(--text-secondary);
  border-color: var(--border-subtle);
}

.btn-outline:hover:not(:disabled) {
  color: var(--text-primary);
  border-color: var(--border-strong);
}

.btn-sm {
  font-size: 0.75rem;
  padding: 0.35rem 0.65rem;
}

.btn-xs {
  font-size: 0.7rem;
  padding: 0.2rem 0.45rem;
}

.btn-icon {
  background: none;
  border: none;
  font-size: 1.15rem;
  cursor: pointer;
  padding: 0.35rem;
  border-radius: var(--radius-sm);
}

.btn-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: #ffffff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

/* ==========================================================================
   Live Metric Cards (5 Core Invariants)
   ========================================================================== */
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 1rem;
}

.metric-card {
  background-color: var(--bg-card);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 1rem;
  display: flex;
  align-items: center;
  gap: 0.85rem;
  transition: transform var(--transition-fast), border-color var(--transition-fast);
}

.metric-card:hover {
  transform: translateY(-2px);
  border-color: var(--border-strong);
}

.metric-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.25rem;
  font-weight: 700;
  font-family: var(--font-mono);
}

.metric-body {
  display: flex;
  flex-direction: column;
}

.metric-label {
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
}

.metric-value {
  font-size: 1.6rem;
  font-weight: 700;
  font-family: var(--font-mono);
  line-height: 1.2;
}

.metric-sub {
  font-size: 0.75rem;
  color: var(--text-secondary);
}

/* Metric Colors */
.card-added .metric-icon { background-color: var(--color-added-bg); color: var(--color-added); }
.card-added .metric-value { color: var(--color-added); }

.card-updated .metric-icon { background-color: var(--color-updated-bg); color: var(--color-updated); }
.card-updated .metric-value { color: var(--color-updated); }

.card-withdrawn .metric-icon { background-color: var(--color-withdrawn-bg); color: var(--color-withdrawn); }
.card-withdrawn .metric-value { color: var(--color-withdrawn); }

.card-skipped .metric-icon { background-color: var(--color-skipped-bg); color: var(--color-skipped); }
.card-skipped .metric-value { color: var(--color-skipped); }

.card-deadletter .metric-icon { background-color: var(--color-deadletter-bg); color: var(--color-deadletter); }
.card-deadletter .metric-value { color: var(--color-deadletter); }

/* ==========================================================================
   Progress & Live Telemetry
   ========================================================================== */
.progress-card {
  padding: 1.25rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.progress-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 0.85rem;
  flex-wrap: wrap;
  gap: 0.75rem;
}

.progress-telemetry {
  display: flex;
  gap: 1.25rem;
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 0.8rem;
}

.progress-track {
  width: 100%;
  height: 12px;
  background-color: var(--bg-input);
  border-radius: 999px;
  overflow: hidden;
  border: 1px solid var(--border-subtle);
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #2563eb, #10b981);
  border-radius: 999px;
  transition: width 300ms ease;
}

/* Event Console */
.event-console {
  background-color: var(--bg-input);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.console-header {
  background-color: rgba(0, 0, 0, 0.2);
  padding: 0.5rem 0.85rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--border-subtle);
  font-size: 0.75rem;
}

.console-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 600;
  color: var(--text-secondary);
}

.console-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.checkbox-inline {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-muted);
  cursor: pointer;
}

.event-log-list {
  height: 140px;
  overflow-y: auto;
  padding: 0.65rem 0.85rem;
  font-family: var(--font-mono);
  font-size: 0.75rem;
  line-height: 1.6;
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
}

.log-entry {
  word-break: break-all;
}

.log-system { color: var(--text-muted); }
.log-start { color: var(--color-running); }
.log-progress { color: var(--text-primary); }
.log-deadletter { color: var(--color-deadletter); font-weight: 600; }
.log-completed { color: var(--color-added); font-weight: 700; }
.log-interrupted { color: var(--color-withdrawn); font-weight: 700; }

/* ==========================================================================
   Data Tables & Explorers
   ========================================================================== */
.table-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.filter-group {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.table-responsive {
  width: 100%;
  overflow-x: auto;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.8rem;
  text-align: left;
}

.data-table th {
  background-color: var(--bg-surface);
  color: var(--text-secondary);
  font-weight: 600;
  padding: 0.65rem 0.85rem;
  border-bottom: 1px solid var(--border-strong);
  white-space: nowrap;
}

.data-table td {
  padding: 0.65rem 0.85rem;
  border-bottom: 1px solid var(--border-subtle);
  vertical-align: middle;
}

.data-table tbody tr:hover {
  background-color: var(--bg-card-hover);
}

.table-empty {
  text-align: center;
  color: var(--text-muted);
  padding: 2rem !important;
}

.sku-cell {
  font-family: var(--font-mono);
  font-weight: 700;
}

.status-badge {
  display: inline-block;
  padding: 0.15rem 0.45rem;
  border-radius: var(--radius-sm);
  font-size: 0.7rem;
  font-weight: 700;
  font-family: var(--font-mono);
}

.status-badge.ACTIVE {
  background-color: var(--color-added-bg);
  color: var(--color-added);
}

.status-badge.WITHDRAWN {
  background-color: var(--color-withdrawn-bg);
  color: var(--color-withdrawn);
}

.defect-badge {
  background-color: var(--color-deadletter-bg);
  color: var(--color-deadletter);
  padding: 0.2rem 0.5rem;
  border-radius: var(--radius-sm);
  font-size: 0.75rem;
  font-weight: 600;
}

.pagination-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 1rem;
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.pagination-buttons {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.page-indicator {
  font-family: var(--font-mono);
}

/* ==========================================================================
   Code Blocks & Modal
   ========================================================================== */
.code-editor {
  width: 100%;
  height: 180px;
  background-color: var(--bg-input);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 0.8rem;
  padding: 0.75rem;
  resize: vertical;
}

.modal-backdrop {
  position: fixed;
  inset: 0;
  background-color: rgba(0, 0, 0, 0.75);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  padding: 1.5rem;
}

.modal-backdrop.hidden {
  display: none;
}

.modal-dialog {
  background-color: var(--bg-surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-lg);
  width: 100%;
  max-width: 650px;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
  box-shadow: var(--shadow-lg);
  animation: modalIn 200ms cubic-bezier(0.16, 1, 0.3, 1);
}

.modal-header {
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--border-subtle);
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.modal-title {
  font-size: 1rem;
  font-weight: 700;
}

.modal-body {
  padding: 1.25rem;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.modal-meta {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  font-size: 0.85rem;
}

.code-tag {
  font-family: var(--font-mono);
  color: var(--text-secondary);
}

.code-block-wrapper {
  background-color: var(--bg-input);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  padding: 1rem;
}

.code-block {
  font-family: var(--font-mono);
  font-size: 0.8rem;
  line-height: 1.5;
  color: #38bdf8;
  overflow-x: auto;
}

.modal-footer {
  padding: 0.85rem 1.25rem;
  border-top: 1px solid var(--border-subtle);
  display: flex;
  justify-content: flex-end;
  gap: 0.75rem;
}

/* ==========================================================================
   Toasts & Utilities
   ========================================================================== */
.toast-container {
  position: fixed;
  bottom: 1.5rem;
  right: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  z-index: 150;
}

.toast {
  background-color: var(--bg-surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  padding: 0.65rem 1rem;
  font-size: 0.8rem;
  box-shadow: var(--shadow-md);
  animation: toastIn 200ms ease;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.toast-success { border-left: 4px solid var(--color-added); }
.toast-error { border-left: 4px solid var(--color-deadletter); }
.toast-info { border-left: 4px solid var(--color-updated); }

.hidden { display: none !important; }

@keyframes spin {
  to { transform: rotate(360deg); }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

@keyframes modalIn {
  from { opacity: 0; transform: scale(0.96); }
  to { opacity: 1; transform: scale(1); }
}

@keyframes toastIn {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

/* ==========================================================================
   Responsive Breakpoints
   ========================================================================== */
@media (max-width: 1024px) {
  .metrics-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 768px) {
  .metrics-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .app-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }
  .header-status {
    width: 100%;
    justify-content: space-between;
  }
  .form-grid {
    grid-template-columns: 1fr;
  }
  .progress-header {
    flex-direction: column;
    align-items: flex-start;
  }
}

@media (max-width: 480px) {
  .metrics-grid {
    grid-template-columns: 1fr;
  }
}
```

---

### 4.6 Dashboard ES6+ JavaScript Application (`web/static/app.js`)

```javascript
/**
 * Coderpush Catalog Sync Engine — Single-Page Dashboard
 * Zero external dependencies: pure vanilla ES6+ JavaScript.
 */

(function () {
  'use strict';

  // --- Central Application State ---
  const state = {
    activeJob: null,
    eventSource: null,
    sseConnected: false,
    sseReconnectTimer: null,
    lastPingTimestamp: Date.now(),
    pingWatchdogTimer: null,
    autoScrollLogs: true,
    theme: localStorage.getItem('cp_theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'),

    // Live Metrics
    metrics: { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 },
    lastDeltas: { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 },

    // Telemetry
    jobStartTime: null,
    runtimeTimer: null,
    lastCommittedBatch: -1,
    totalBatches: 0,
    recordsProcessed: 0,

    // Explorers
    dlq: { items: [], total: 0, limit: 10, offset: 0, filterJob: '' },
    catalog: { items: [], total: 0, limit: 10, offset: 0, search: '', status: '' },
    history: { jobs: [] },
  };

  // --- Preset Configurations ---
  const PRESETS = {
    default: { seed: 42, batches: 10, size: 100, update: 20, dup: 5, ooo: 10, with: 5, inv: 5 },
    clean:   { seed: 101, batches: 10, size: 100, update: 20, dup: 0, ooo: 0, with: 0, inv: 0 },
    stress:  { seed: 999, batches: 15, size: 150, update: 15, dup: 10, ooo: 15, with: 10, inv: 15 },
    large:   { seed: 777, batches: 50, size: 200, update: 20, dup: 5, ooo: 10, with: 5, inv: 5 },
  };

  // --- DOM Element References ---
  const el = {
    themeToggle: document.getElementById('theme-toggle'),
    themeIcon: document.getElementById('theme-icon'),
    activeJobBadge: document.getElementById('active-job-badge'),
    activeJobText: document.getElementById('active-job-text'),
    sseStatus: document.getElementById('sse-status'),
    sseStatusText: document.getElementById('sse-status-text'),

    // Control Form
    generatorForm: document.getElementById('generator-form'),
    inputSeed: document.getElementById('input-seed'),
    btnRandomSeed: document.getElementById('btn-random-seed'),
    inputBatches: document.getElementById('input-batches'),
    inputBatchSize: document.getElementById('input-batch-size'),
    inputUpdateRatio: document.getElementById('input-update-ratio'),
    inputDupRatio: document.getElementById('input-dup-ratio'),
    inputOooRatio: document.getElementById('input-ooo-ratio'),
    inputWithdrawnRatio: document.getElementById('input-withdrawn-ratio'),
    inputInvalidRatio: document.getElementById('input-invalid-ratio'),
    valUpdateRatio: document.getElementById('val-update-ratio'),
    valDupRatio: document.getElementById('val-dup-ratio'),
    valOooRatio: document.getElementById('val-ooo-ratio'),
    valWithdrawnRatio: document.getElementById('val-withdrawn-ratio'),
    valInvalidRatio: document.getElementById('val-invalid-ratio'),
    ratioValidator: document.getElementById('ratio-validator'),
    totalRatioVal: document.getElementById('total-ratio-val'),
    btnStartJob: document.getElementById('btn-start-job'),
    btnResetForm: document.getElementById('btn-reset-form'),

    // Resume Tab
    selectInterruptedJob: document.getElementById('select-interrupted-job'),
    btnRefreshInterrupted: document.getElementById('btn-refresh-interrupted'),
    btnResumeJob: document.getElementById('btn-resume-job'),
    resumeJobDetails: document.getElementById('resume-job-details'),

    // Manual Tab
    manualJsonInput: document.getElementById('manual-json-input'),
    btnLoadSampleJson: document.getElementById('btn-load-sample-json'),
    btnClearJson: document.getElementById('btn-clear-json'),
    btnSubmitManual: document.getElementById('btn-submit-manual'),
    jsonParseStatus: document.getElementById('json-parse-status'),

    // Metric Values
    metricAdded: document.getElementById('metric-added'),
    metricUpdated: document.getElementById('metric-updated'),
    metricWithdrawn: document.getElementById('metric-withdrawn'),
    metricSkipped: document.getElementById('metric-skipped'),
    metricDeadLetter: document.getElementById('metric-deadletter'),
    deltaAdded: document.getElementById('delta-added'),
    deltaUpdated: document.getElementById('delta-updated'),
    deltaWithdrawn: document.getElementById('delta-withdrawn'),
    deltaSkipped: document.getElementById('delta-skipped'),
    deltaDeadLetter: document.getElementById('delta-deadletter'),

    // Progress & Telemetry
    progressBarFill: document.getElementById('progress-bar-fill'),
    batchProgressText: document.getElementById('batch-progress-text'),
    telemetrySpeed: document.getElementById('telemetry-speed'),
    telemetryEta: document.getElementById('telemetry-eta'),
    telemetryRuntime: document.getElementById('telemetry-runtime'),
    eventLogContainer: document.getElementById('event-log-container'),
    checkAutoscroll: document.getElementById('check-autoscroll'),
    btnClearConsole: document.getElementById('btn-clear-console'),

    // Explorers
    countDlqBadge: document.getElementById('count-dlq-badge'),
    countCatalogBadge: document.getElementById('count-catalog-badge'),
    filterDlqJob: document.getElementById('filter-dlq-job'),
    btnRefreshDlq: document.getElementById('btn-refresh-dlq'),
    dlqTableBody: document.getElementById('dlq-table-body'),
    dlqPaginationInfo: document.getElementById('dlq-pagination-info'),
    dlqPageNum: document.getElementById('dlq-page-num'),
    btnDlqPrev: document.getElementById('btn-dlq-prev'),
    btnDlqNext: document.getElementById('btn-dlq-next'),

    filterProductSearch: document.getElementById('filter-product-search'),
    filterProductStatus: document.getElementById('filter-product-status'),
    btnRefreshCatalog: document.getElementById('btn-refresh-catalog'),
    catalogTableBody: document.getElementById('catalog-table-body'),
    catalogPaginationInfo: document.getElementById('catalog-pagination-info'),
    catalogPageNum: document.getElementById('catalog-page-num'),
    btnCatalogPrev: document.getElementById('btn-catalog-prev'),
    btnCatalogNext: document.getElementById('btn-catalog-next'),

    btnRefreshHistory: document.getElementById('btn-refresh-history'),
    historyTableBody: document.getElementById('history-table-body'),

    // Modal
    payloadModal: document.getElementById('payload-modal'),
    modalIssueId: document.getElementById('modal-issue-id'),
    modalDefectReason: document.getElementById('modal-defect-reason'),
    modalPayloadCode: document.getElementById('modal-payload-code'),
    btnCloseModal: document.getElementById('btn-close-modal'),
    btnDismissModal: document.getElementById('btn-dismiss-modal'),
    btnCopyPayload: document.getElementById('btn-copy-payload'),

    // Toasts
    toastContainer: document.getElementById('toast-container'),
  };

  // --- Theme Management ---
  function applyTheme(theme) {
    state.theme = theme;
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('cp_theme', theme);
    el.themeIcon.textContent = theme === 'dark' ? '🌙' : '☀';
  }

  el.themeToggle.addEventListener('click', () => {
    applyTheme(state.theme === 'dark' ? 'light' : 'dark');
  });

  // --- Tab Navigation ---
  document.querySelectorAll('.tab-group').forEach((group) => {
    group.addEventListener('click', (e) => {
      const btn = e.target.closest('.tab-btn');
      if (!btn) return;
      const targetTabId = btn.getAttribute('data-tab');
      const parentCard = group.closest('.card');

      group.querySelectorAll('.tab-btn').forEach((b) => b.classList.remove('active'));
      parentCard.querySelectorAll('.tab-content').forEach((c) => c.classList.remove('active'));

      btn.classList.add('active');
      const content = document.getElementById(targetTabId);
      if (content) content.classList.add('active');

      if (targetTabId === 'tab-dlq') loadDLQ();
      if (targetTabId === 'tab-catalog') loadCatalog();
      if (targetTabId === 'tab-history') loadHistory();
      if (targetTabId === 'tab-resume') loadInterruptedJobs();
    });
  });

  // --- Ratio Validator ---
  function updateRatios() {
    const updateVal = parseInt(el.inputUpdateRatio.value, 10);
    const dupVal = parseInt(el.inputDupRatio.value, 10);
    const oooVal = parseInt(el.inputOooRatio.value, 10);
    const withVal = parseInt(el.inputWithdrawnRatio.value, 10);
    const invVal = parseInt(el.inputInvalidRatio.value, 10);

    el.valUpdateRatio.textContent = `${updateVal}%`;
    el.valDupRatio.textContent = `${dupVal}%`;
    el.valOooRatio.textContent = `${oooVal}%`;
    el.valWithdrawnRatio.textContent = `${withVal}%`;
    el.valInvalidRatio.textContent = `${invVal}%`;

    const total = updateVal + dupVal + oooVal + withVal + invVal;
    el.totalRatioVal.textContent = `${total}%`;

    if (total > 100) {
      el.ratioValidator.className = 'ratio-validator invalid';
      el.btnStartJob.disabled = true;
      el.totalRatioVal.textContent = `${total}% (EXCEEDS 100%)`;
    } else {
      el.ratioValidator.className = 'ratio-validator valid';
      if (!state.activeJob || state.activeJob.status !== 'RUNNING') {
        el.btnStartJob.disabled = false;
      }
    }
  }

  [el.inputUpdateRatio, el.inputDupRatio, el.inputOooRatio, el.inputWithdrawnRatio, el.inputInvalidRatio].forEach((input) => {
    input.addEventListener('input', updateRatios);
  });

  // --- Preset Applicator ---
  function applyPreset(key) {
    const p = PRESETS[key];
    if (!p) return;
    el.inputSeed.value = p.seed;
    el.inputBatches.value = p.batches;
    el.inputBatchSize.value = p.size;
    el.inputUpdateRatio.value = p.update;
    el.inputDupRatio.value = p.dup;
    el.inputOooRatio.value = p.ooo;
    el.inputWithdrawnRatio.value = p.with;
    el.inputInvalidRatio.value = p.inv;
    updateRatios();
  }

  document.querySelectorAll('.preset-btn').forEach((btn) => {
    btn.addEventListener('click', () => applyPreset(btn.getAttribute('data-preset')));
  });

  el.btnRandomSeed.addEventListener('click', () => {
    el.inputSeed.value = Math.floor(Math.random() * 1000000);
  });

  el.btnResetForm.addEventListener('click', () => {
    applyPreset('default');
  });

  // --- Toast Notifications ---
  function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.innerHTML = `<span>${message}</span>`;
    el.toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      toast.style.transition = 'all 200ms ease';
      setTimeout(() => toast.remove(), 200);
    }, 4000);
  }

  // --- Log Stream Appender ---
  function appendLog(text, category = 'system') {
    const now = new Date();
    const timeStr = now.toTimeString().split(' ')[0] + '.' + String(now.getMilliseconds()).padStart(3, '0');
    const entry = document.createElement('div');
    entry.className = `log-entry log-${category}`;
    entry.textContent = `[${timeStr}] ${text}`;
    el.eventLogContainer.appendChild(entry);

    // Limit log size to 150 items
    while (el.eventLogContainer.children.length > 150) {
      el.eventLogContainer.removeChild(el.eventLogContainer.firstChild);
    }

    if (state.autoScrollLogs) {
      el.eventLogContainer.scrollTop = el.eventLogContainer.scrollHeight;
    }
  }

  el.checkAutoscroll.addEventListener('change', (e) => {
    state.autoScrollLogs = e.target.checked;
  });

  el.btnClearConsole.addEventListener('click', () => {
    el.eventLogContainer.innerHTML = '';
    appendLog('Log cleared by user.', 'system');
  });

  // --- SSE Event Stream Connection ---
  function initSSE() {
    if (state.eventSource) {
      state.eventSource.close();
    }

    el.sseStatus.className = 'badge badge-sse sse-connecting';
    el.sseStatusText.textContent = 'Connecting SSE...';

    state.eventSource = new EventSource('/api/events');

    state.eventSource.onopen = () => {
      state.sseConnected = true;
      state.lastPingTimestamp = Date.now();
      el.sseStatus.className = 'badge badge-sse sse-connected';
      el.sseStatusText.textContent = 'Live SSE Connected';
      appendLog('Connected to Server-Sent Events stream.', 'system');
    };

    state.eventSource.onerror = () => {
      state.sseConnected = false;
      el.sseStatus.className = 'badge badge-sse sse-disconnected';
      el.sseStatusText.textContent = 'Reconnecting...';
    };

    // Keepalive ping watchdog: trigger reconnect if no ping or message in 35s
    clearInterval(state.pingWatchdogTimer);
    state.pingWatchdogTimer = setInterval(() => {
      if (Date.now() - state.lastPingTimestamp > 35000 && state.sseConnected) {
        appendLog('Keepalive timeout. Forcing SSE reconnection...', 'system');
        initSSE();
      }
    }, 5000);

    // Register Event Listeners
    state.eventSource.addEventListener('job_started', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleJobStarted(data);
      } catch (err) {
        console.error('Invalid job_started payload', err);
      }
    });

    state.eventSource.addEventListener('progress', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleProgress(data);
      } catch (err) {
        console.error('Invalid progress payload', err);
      }
    });

    state.eventSource.addEventListener('dead_letter', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleDeadLetter(data);
      } catch (err) {
        console.error('Invalid dead_letter payload', err);
      }
    });

    state.eventSource.addEventListener('job_completed', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleJobCompleted(data);
      } catch (err) {
        console.error('Invalid job_completed payload', err);
      }
    });

    state.eventSource.addEventListener('job_interrupted', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleJobInterrupted(data);
      } catch (err) {
        console.error('Invalid job_interrupted payload', err);
      }
    });
  }

  // --- Runtime Timer ---
  function startRuntimeTimer() {
    state.jobStartTime = Date.now();
    clearInterval(state.runtimeTimer);
    state.runtimeTimer = setInterval(() => {
      if (!state.jobStartTime) return;
      const elapsedSec = Math.floor((Date.now() - state.jobStartTime) / 1000);
      const mm = String(Math.floor(elapsedSec / 60)).padStart(2, '0');
      const ss = String(elapsedSec % 60).padStart(2, '0');
      el.telemetryRuntime.textContent = `${mm}:${ss}`;
    }, 1000);
  }

  function stopRuntimeTimer() {
    clearInterval(state.runtimeTimer);
    state.runtimeTimer = null;
  }

  // --- SSE Event Handlers ---
  function handleJobStarted(job) {
    state.activeJob = job;
    state.totalBatches = job.total_batches || 10;
    state.recordsProcessed = 0;
    state.metrics = { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 };
    updateMetricCards();
    startRuntimeTimer();

    el.activeJobBadge.className = 'badge badge-job status-running';
    el.activeJobText.textContent = `Job #${job.job_id} (RUNNING)`;
    el.btnStartJob.disabled = true;
    el.btnResumeJob.disabled = true;
    el.progressBarFill.style.width = '0%';
    el.batchProgressText.textContent = `0 / ${state.totalBatches} Batches (0.0%)`;

    appendLog(`Job #${job.job_id} started: ${job.total_batches} total batches.`, 'start');
    showToast(`Job ${job.job_id} started`, 'info');
  }

  function handleProgress(data) {
    state.lastPingTimestamp = Date.now();
    const batchIndex = data.batch_index;
    const totalBatches = data.total_batches || state.totalBatches;
    const summary = data.summary || data.cumulative_summary || {};
    const delta = data.batch_delta || {};

    state.lastCommittedBatch = batchIndex;
    state.totalBatches = totalBatches;

    // Update state metrics
    if (summary.added !== undefined) state.metrics.added = summary.added;
    if (summary.updated !== undefined) state.metrics.updated = summary.updated;
    if (summary.withdrawn !== undefined) state.metrics.withdrawn = summary.withdrawn;
    if (summary.skipped !== undefined) state.metrics.skipped = summary.skipped;
    if (summary.dead_letter !== undefined) state.metrics.deadLetter = summary.dead_letter;

    // Update deltas
    state.lastDeltas = delta;

    updateMetricCards();

    // Calculate percentage
    const pct = totalBatches > 0 ? (((batchIndex + 1) / totalBatches) * 100).toFixed(1) : '0.0';
    el.progressBarFill.style.width = `${pct}%`;
    el.batchProgressText.textContent = `${batchIndex + 1} / ${totalBatches} Batches (${pct}%)`;

    // Calculate throughput & ETA
    if (state.jobStartTime) {
      const elapsedSec = (Date.now() - state.jobStartTime) / 1000;
      const totalRecs = state.metrics.added + state.metrics.updated + state.metrics.withdrawn + state.metrics.skipped + state.metrics.deadLetter;
      if (elapsedSec > 0.5) {
        const speed = Math.round(totalRecs / elapsedSec);
        el.telemetrySpeed.textContent = `${speed} rec/s`;
        const batchesRemaining = totalBatches - (batchIndex + 1);
        if (batchesRemaining > 0 && batchIndex > 0) {
          const avgBatchTime = elapsedSec / (batchIndex + 1);
          const etaSec = Math.round(batchesRemaining * avgBatchTime);
          el.telemetryEta.textContent = `${etaSec}s`;
        } else {
          el.telemetryEta.textContent = '0s';
        }
      }
    }

    appendLog(`Batch ${batchIndex} committed: +${delta.added || 0} added, ~${delta.updated || 0} updated, -${delta.withdrawn || 0} withdrawn, ↷${delta.skipped || 0} skipped, ⚠${delta.dead_letter || 0} DLQ.`, 'progress');
  }

  function handleDeadLetter(issue) {
    appendLog(`[DLQ QUARANTINE] Job #${issue.job_id} Batch ${issue.batch_index} SKU='${issue.sku || 'EMPTY'}': ${issue.reason}`, 'deadletter');
    loadDLQ();
  }

  function handleJobCompleted(data) {
    stopRuntimeTimer();
    el.activeJobBadge.className = 'badge badge-job status-completed';
    el.activeJobText.textContent = `Job #${data.job_id} (COMPLETED)`;
    el.progressBarFill.style.width = '100%';
    el.batchProgressText.textContent = `${state.totalBatches} / ${state.totalBatches} Batches (100%)`;
    el.btnStartJob.disabled = false;
    el.telemetryEta.textContent = 'Done';

    appendLog(`Job #${data.job_id} COMPLETED successfully.`, 'completed');
    showToast(`Job ${data.job_id} completed successfully!`, 'success');
    loadCatalog();
    loadHistory();
    loadInterruptedJobs();
  }

  function handleJobInterrupted(data) {
    stopRuntimeTimer();
    el.activeJobBadge.className = 'badge badge-job status-interrupted';
    el.activeJobText.textContent = `Job #${data.job_id} (INTERRUPTED)`;
    el.btnStartJob.disabled = false;
    el.telemetryEta.textContent = 'Interrupted';

    appendLog(`Job #${data.job_id} INTERRUPTED at batch index ${data.committed_batch_index}. Reason: ${data.reason || 'interrupted'}`, 'interrupted');
    showToast(`Job ${data.job_id} was interrupted!`, 'error');
    loadInterruptedJobs();
    loadHistory();
  }

  // --- Metric Cards Animation ---
  function updateMetricCards() {
    el.metricAdded.textContent = state.metrics.added.toLocaleString();
    el.metricUpdated.textContent = state.metrics.updated.toLocaleString();
    el.metricWithdrawn.textContent = state.metrics.withdrawn.toLocaleString();
    el.metricSkipped.textContent = state.metrics.skipped.toLocaleString();
    el.metricDeadLetter.textContent = state.metrics.deadLetter.toLocaleString();

    el.deltaAdded.textContent = `+${state.lastDeltas.added || 0}`;
    el.deltaUpdated.textContent = `+${state.lastDeltas.updated || 0}`;
    el.deltaWithdrawn.textContent = `+${state.lastDeltas.withdrawn || 0}`;
    el.deltaSkipped.textContent = `+${state.lastDeltas.skipped || 0}`;
    el.deltaDeadLetter.textContent = `+${state.lastDeltas.dead_letter || 0}`;

    el.countDlqBadge.textContent = state.metrics.deadLetter;
  }

  // --- REST Actions: Start Sync Job ---
  el.generatorForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const payload = {
      mode: 'generator',
      seed: parseInt(el.inputSeed.value, 10),
      total_batches: parseInt(el.inputBatches.value, 10),
      batch_size: parseInt(el.inputBatchSize.value, 10),
      update_ratio: parseFloat(el.inputUpdateRatio.value) / 100.0,
      duplicate_ratio: parseFloat(el.inputDupRatio.value) / 100.0,
      out_of_order_ratio: parseFloat(el.inputOooRatio.value) / 100.0,
      withdrawn_ratio: parseFloat(el.inputWithdrawnRatio.value) / 100.0,
      invalid_ratio: parseFloat(el.inputInvalidRatio.value) / 100.0,
    };

    el.btnStartJob.disabled = true;
    const spinner = el.btnStartJob.querySelector('.btn-spinner');
    if (spinner) spinner.classList.remove('hidden');

    try {
      const resp = await fetch('/api/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      const body = await resp.json();
      if (resp.status === 202 || resp.status === 200) {
        showToast(`Job started: ${body.job_id}`, 'success');
      } else if (resp.status === 409) {
        showToast(body.error || 'A job is already running!', 'error');
        el.btnStartJob.disabled = false;
      } else {
        showToast(body.error || 'Failed to start job', 'error');
        el.btnStartJob.disabled = false;
      }
    } catch (err) {
      showToast(`Network error: ${err.message}`, 'error');
      el.btnStartJob.disabled = false;
    } finally {
      if (spinner) spinner.classList.add('hidden');
    }
  });

  // --- REST Actions: Resume Interrupted Job ---
  async function loadInterruptedJobs() {
    try {
      const resp = await fetch('/api/jobs?status=INTERRUPTED&limit=50');
      if (!resp.ok) return;
      const data = await resp.json();
      const jobs = data.jobs || [];

      el.selectInterruptedJob.innerHTML = '';
      if (jobs.length === 0) {
        el.selectInterruptedJob.innerHTML = '<option value="">No interrupted jobs found</option>';
        el.btnResumeJob.disabled = true;
        el.resumeJobDetails.classList.add('hidden');
        return;
      }

      el.selectInterruptedJob.innerHTML = '<option value="">-- Select an interrupted job --</option>';
      jobs.forEach((j) => {
        const opt = document.createElement('option');
        opt.value = j.job_id;
        opt.textContent = `${j.job_id} | Committed: ${j.committed_batch_index + 1}/${j.total_batches} | ${new Date(j.created_at).toLocaleTimeString()}`;
        opt.dataset.job = JSON.stringify(j);
        el.selectInterruptedJob.appendChild(opt);
      });
      el.btnResumeJob.disabled = true;
    } catch (err) {
      console.error('Error fetching interrupted jobs', err);
    }
  }

  el.btnRefreshInterrupted.addEventListener('click', loadInterruptedJobs);

  el.selectInterruptedJob.addEventListener('change', () => {
    const selectedOpt = el.selectInterruptedJob.selectedOptions[0];
    if (!selectedOpt || !selectedOpt.value) {
      el.btnResumeJob.disabled = true;
      el.resumeJobDetails.classList.add('hidden');
      return;
    }
    el.btnResumeJob.disabled = false;
    const job = JSON.parse(selectedOpt.dataset.job);
    el.resumeJobDetails.innerHTML = `
      <div class="job-meta-box">
        <strong>Target Job:</strong> ${job.job_id} | 
        <strong>Last Committed Batch:</strong> ${job.committed_batch_index} | 
        <strong>Total Batches:</strong> ${job.total_batches}
      </div>
    `;
    el.resumeJobDetails.classList.remove('hidden');
  });

  el.btnResumeJob.addEventListener('click', async () => {
    const jobID = el.selectInterruptedJob.value;
    if (!jobID) return;

    el.btnResumeJob.disabled = true;
    const spinner = el.btnResumeJob.querySelector('.btn-spinner');
    if (spinner) spinner.classList.remove('hidden');

    try {
      const resp = await fetch(`/api/jobs/${encodeURIComponent(jobID)}/resume`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      const body = await resp.json();
      if (resp.status === 202 || resp.status === 200) {
        showToast(`Resumed job ${jobID}`, 'success');
      } else {
        showToast(body.error || 'Failed to resume job', 'error');
        el.btnResumeJob.disabled = false;
      }
    } catch (err) {
      showToast(`Network error: ${err.message}`, 'error');
      el.btnResumeJob.disabled = false;
    } finally {
      if (spinner) spinner.classList.add('hidden');
    }
  });

  // --- REST Actions: Manual Custom Batch JSON ---
  el.btnLoadSampleJson.addEventListener('click', () => {
    const sample = [
      {
        sku: 'CUSTOM-KEYBOARD-01',
        name: 'Wireless Mechanical Keyboard',
        price: 99.50,
        status: 'ACTIVE',
        timestamp: new Date().toISOString(),
      },
      {
        sku: 'CUSTOM-MOUSE-02',
        name: 'Ergonomic Precision Mouse',
        price: 49.00,
        status: 'ACTIVE',
        timestamp: new Date().toISOString(),
      },
    ];
    el.manualJsonInput.value = JSON.stringify(sample, null, 2);
  });

  el.btnClearJson.addEventListener('click', () => {
    el.manualJsonInput.value = '';
    el.jsonParseStatus.textContent = '';
  });

  el.btnSubmitManual.addEventListener('click', async () => {
    const text = el.manualJsonInput.value.trim();
    if (!text) {
      showToast('Please enter a valid JSON array of records', 'error');
      return;
    }
    let records;
    try {
      records = JSON.parse(text);
      if (!Array.isArray(records)) {
        throw new Error('Payload must be a JSON array of records');
      }
    } catch (err) {
      el.jsonParseStatus.textContent = `Invalid JSON: ${err.message}`;
      el.jsonParseStatus.style.color = 'var(--color-deadletter)';
      return;
    }

    const payload = {
      mode: 'batches',
      batches: [{ batch_index: 0, records: records }],
    };

    try {
      const resp = await fetch('/api/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const body = await resp.json();
      if (resp.status === 202 || resp.status === 200) {
        showToast(`Custom batch job started: ${body.job_id}`, 'success');
        el.manualJsonInput.value = '';
        el.jsonParseStatus.textContent = '';
      } else {
        showToast(body.error || 'Failed to submit batch', 'error');
      }
    } catch (err) {
      showToast(`Network error: ${err.message}`, 'error');
    }
  });

  // --- Explorer: Dead-Letter Queue Table ---
  async function loadDLQ() {
    const jobID = el.filterDlqJob.value.trim();
    const url = `/api/dead-letter?limit=${state.dlq.limit}&offset=${state.dlq.offset}${jobID ? `&job_id=${encodeURIComponent(jobID)}` : ''}`;

    try {
      const resp = await fetch(url);
      if (!resp.ok) return;
      const data = await resp.json();
      const issues = data.issues || [];
      state.dlq.total = data.total || 0;
      el.countDlqBadge.textContent = state.dlq.total;

      el.dlqTableBody.innerHTML = '';
      if (issues.length === 0) {
        el.dlqTableBody.innerHTML = '<tr><td colspan="7" class="table-empty">No corrupt records in Dead-Letter Queue.</td></tr>';
        el.dlqPaginationInfo.textContent = 'Showing 0-0 of 0';
        el.btnDlqPrev.disabled = true;
        el.btnDlqNext.disabled = true;
        return;
      }

      issues.forEach((issue) => {
        const tr = document.createElement('tr');
        const shortIssueId = issue.issue_id.length > 16 ? `${issue.issue_id.substring(0, 16)}...` : issue.issue_id;
        const timeStr = new Date(issue.created_at).toLocaleTimeString();

        tr.innerHTML = `
          <td><span class="code-tag" title="${issue.issue_id}">${shortIssueId}</span></td>
          <td>${issue.job_id}</td>
          <td>${issue.batch_index}</td>
          <td class="sku-cell">${issue.sku ? issue.sku : '<span class="defect-badge">[EMPTY]</span>'}</td>
          <td><span class="defect-badge">${issue.reason}</span></td>
          <td>${timeStr}</td>
          <td><button class="btn btn-xs btn-outline btn-inspect" data-issue='${JSON.stringify(issue).replace(/'/g, '&apos;')}'>Inspect</button></td>
        `;
        el.dlqTableBody.appendChild(tr);
      });

      // Pagination
      const from = state.dlq.offset + 1;
      const to = Math.min(state.dlq.offset + state.dlq.limit, state.dlq.total);
      el.dlqPaginationInfo.textContent = `Showing ${from}-${to} of ${state.dlq.total}`;
      const curPage = Math.floor(state.dlq.offset / state.dlq.limit) + 1;
      const totalPages = Math.ceil(state.dlq.total / state.dlq.limit) || 1;
      el.dlqPageNum.textContent = `Page ${curPage} of ${totalPages}`;

      el.btnDlqPrev.disabled = state.dlq.offset <= 0;
      el.btnDlqNext.disabled = state.dlq.offset + state.dlq.limit >= state.dlq.total;
    } catch (err) {
      console.error('Failed to load DLQ', err);
    }
  }

  el.btnRefreshDlq.addEventListener('click', loadDLQ);
  el.filterDlqJob.addEventListener('change', () => {
    state.dlq.offset = 0;
    loadDLQ();
  });

  el.btnDlqPrev.addEventListener('click', () => {
    if (state.dlq.offset >= state.dlq.limit) {
      state.dlq.offset -= state.dlq.limit;
      loadDLQ();
    }
  });

  el.btnDlqNext.addEventListener('click', () => {
    if (state.dlq.offset + state.dlq.limit < state.dlq.total) {
      state.dlq.offset += state.dlq.limit;
      loadDLQ();
    }
  });

  // Modal Payload Inspector
  el.dlqTableBody.addEventListener('click', (e) => {
    const btn = e.target.closest('.btn-inspect');
    if (!btn) return;
    const issue = JSON.parse(btn.getAttribute('data-issue'));
    openPayloadModal(issue);
  });

  function openPayloadModal(issue) {
    el.modalIssueId.textContent = issue.issue_id;
    el.modalDefectReason.textContent = issue.reason;
    try {
      const parsed = JSON.parse(issue.raw_payload);
      el.modalPayloadCode.textContent = JSON.stringify(parsed, null, 2);
    } catch (_) {
      el.modalPayloadCode.textContent = issue.raw_payload;
    }
    el.payloadModal.classList.remove('hidden');
  }

  function closePayloadModal() {
    el.payloadModal.classList.add('hidden');
  }

  el.btnCloseModal.addEventListener('click', closePayloadModal);
  el.btnDismissModal.addEventListener('click', closePayloadModal);
  el.payloadModal.addEventListener('click', (e) => {
    if (e.target === el.payloadModal) closePayloadModal();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !el.payloadModal.classList.contains('hidden')) {
      closePayloadModal();
    }
  });

  el.btnCopyPayload.addEventListener('click', () => {
    const code = el.modalPayloadCode.textContent;
    navigator.clipboard.writeText(code).then(() => {
      showToast('Payload copied to clipboard!', 'info');
    });
  });

  // --- Explorer: Catalog Products Table ---
  let searchDebounceTimer = null;
  async function loadCatalog() {
    const search = el.filterProductSearch.value.trim();
    const status = el.filterProductStatus.value;
    const url = `/api/products?limit=${state.catalog.limit}&offset=${state.catalog.offset}${search ? `&search=${encodeURIComponent(search)}` : ''}${status ? `&status=${encodeURIComponent(status)}` : ''}`;

    try {
      const resp = await fetch(url);
      if (!resp.ok) return;
      const data = await resp.json();
      const products = data.products || [];
      state.catalog.total = data.total || 0;
      el.countCatalogBadge.textContent = state.catalog.total.toLocaleString();

      el.catalogTableBody.innerHTML = '';
      if (products.length === 0) {
        el.catalogTableBody.innerHTML = '<tr><td colspan="6" class="table-empty">No products match current filters.</td></tr>';
        el.catalogPaginationInfo.textContent = 'Showing 0-0 of 0';
        el.btnCatalogPrev.disabled = true;
        el.btnCatalogNext.disabled = true;
        return;
      }

      products.forEach((p) => {
        const tr = document.createElement('tr');
        const priceStr = `$${Number(p.price).toFixed(2)}`;
        const supTimeStr = new Date(p.supplier_timestamp).toLocaleTimeString();
        const updTimeStr = new Date(p.updated_at).toLocaleTimeString();

        tr.innerHTML = `
          <td class="sku-cell">${p.sku}</td>
          <td>${p.name}</td>
          <td><strong>${priceStr}</strong></td>
          <td><span class="status-badge ${p.status}">${p.status}</span></td>
          <td>${supTimeStr}</td>
          <td>${updTimeStr}</td>
        `;
        el.catalogTableBody.appendChild(tr);
      });

      const from = state.catalog.offset + 1;
      const to = Math.min(state.catalog.offset + state.catalog.limit, state.catalog.total);
      el.catalogPaginationInfo.textContent = `Showing ${from}-${to} of ${state.catalog.total.toLocaleString()}`;
      const curPage = Math.floor(state.catalog.offset / state.catalog.limit) + 1;
      const totalPages = Math.ceil(state.catalog.total / state.catalog.limit) || 1;
      el.catalogPageNum.textContent = `Page ${curPage} of ${totalPages}`;

      el.btnCatalogPrev.disabled = state.catalog.offset <= 0;
      el.btnCatalogNext.disabled = state.catalog.offset + state.catalog.limit >= state.catalog.total;
    } catch (err) {
      console.error('Failed to load catalog products', err);
    }
  }

  el.btnRefreshCatalog.addEventListener('click', loadCatalog);
  el.filterProductStatus.addEventListener('change', () => {
    state.catalog.offset = 0;
    loadCatalog();
  });

  el.filterProductSearch.addEventListener('input', () => {
    clearTimeout(searchDebounceTimer);
    searchDebounceTimer = setTimeout(() => {
      state.catalog.offset = 0;
      loadCatalog();
    }, 250);
  });

  el.btnCatalogPrev.addEventListener('click', () => {
    if (state.catalog.offset >= state.catalog.limit) {
      state.catalog.offset -= state.catalog.limit;
      loadCatalog();
    }
  });

  el.btnCatalogNext.addEventListener('click', () => {
    if (state.catalog.offset + state.catalog.limit < state.catalog.total) {
      state.catalog.offset += state.catalog.limit;
      loadCatalog();
    }
  });

  // --- Explorer: History Table ---
  async function loadHistory() {
    try {
      const resp = await fetch('/api/jobs?limit=20&offset=0');
      if (!resp.ok) return;
      const data = await resp.json();
      const jobs = data.jobs || [];

      el.historyTableBody.innerHTML = '';
      if (jobs.length === 0) {
        el.historyTableBody.innerHTML = '<tr><td colspan="9" class="table-empty">No sync jobs recorded.</td></tr>';
        return;
      }

      jobs.forEach((j) => {
        const tr = document.createElement('tr');
        const s = j.summary || {};
        const timeStr = new Date(j.created_at).toLocaleString();

        tr.innerHTML = `
          <td><span class="code-tag">${j.job_id}</span></td>
          <td><span class="status-badge ${j.status}">${j.status}</span></td>
          <td>${j.committed_batch_index + 1} / ${j.total_batches}</td>
          <td style="color:var(--color-added)">+${s.added || 0}</td>
          <td style="color:var(--color-updated)">~${s.updated || 0}</td>
          <td style="color:var(--color-withdrawn)">-${s.withdrawn || 0}</td>
          <td style="color:var(--color-skipped)">↷${s.skipped || 0}</td>
          <td style="color:var(--color-deadletter)">⚠${s.dead_letter || 0}</td>
          <td>${timeStr}</td>
        `;
        el.historyTableBody.appendChild(tr);
      });
    } catch (err) {
      console.error('Failed to load history', err);
    }
  }

  el.btnRefreshHistory.addEventListener('click', loadHistory);

  // --- Initial Bootstrap ---
  function init() {
    applyTheme(state.theme);
    updateRatios();
    initSSE();
    loadDLQ();
    loadCatalog();
    loadInterruptedJobs();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
```

---

## 5. Verification Method

To independently verify the static asset handler and UI architecture designs:

1. **Unit Test Suite for Embedded Asset Handling**:
   - Write `web/embed_test.go` and run:
     ```bash
     go test -v ./web/...
     ```
   - **Expected Result**: PASS with 100% statement coverage for `web/embed.go`. Asserts valid HTTP 200 responses, correct MIME types (`text/html`, `text/css`, `application/javascript`), SPA fallback for unknown non-API routes, and 404 for unhandled API routes.

2. **Race Detection and Coverage Verification**:
   - Run the project test suite under multi-iteration race detection:
     ```bash
     go test -race -count=5 ./web/...
     ```
   - **Expected Result**: 0 data races.

3. **Standalone Server Integration Verification**:
   - Once `cmd/server/main.go` and `web/` are compiled:
     ```bash
     go run ./cmd/server --port 8080 --db :memory:
     ```
   - Query endpoints with curl:
     ```bash
     # Verify embedded index.html
     curl -i http://localhost:8080/ | grep "Coderpush Catalog Sync Engine"
     
     # Verify CSS
     curl -i http://localhost:8080/style.css | grep "text/css"
     
     # Verify JavaScript
     curl -i http://localhost:8080/app.js | grep "javascript"
     
     # Verify SPA Fallback
     curl -i http://localhost:8080/dashboard/test | grep "Coderpush Catalog Sync Engine"
     ```

4. **Invalidation Conditions**:
   - The embedded static asset mechanism is invalidated if `web/embed.go` cannot compile under Go 1.16+, if files in `web/static/` require Node.js/npm dependencies, or if MIME types are missing in minimal Docker container environments.
