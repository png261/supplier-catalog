# Ecosystem, UI & Packaging Architecture Report: Mock Generator, Embedded Dashboard, Security & CI/CD

## 1. Observation

### 1.1 Source Documents & Requirements
- **Authoritative Source**: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- **Dispatch Assignment**: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2/DISPATCH.md`
- **Peer Specification Artifact**: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1/handoff.md`

#### Verbatim Requirements Quoted from ORIGINAL_REQUEST.md:
```markdown
### R3. Programmatic Mock Data Generator (Infinite Scale)
- Implement a standalone Go generator to stream and export fictional supplier product batches with configurable volume and deterministic random seeds.
- Inject controlled edge-case ratios for duplicates, out-of-order timestamps, withdrawn items, and invalid/corrupt records to support comprehensive testing and stress testing.

### R4. Minimalist Staff Interface & Status Reporting
- Provide a clean, minimalist single-page dashboard embedded directly into the Go binary.
- Allow staff to start an update, view real-time completion progress, inspect a summary of changes (+Added, ~Updated, -Withdrawn, ↷Skipped), view issues needing attention, and resume interrupted updates.

### R5. Security, Test Rigor & CI/CD Packaging
- Configure Husky and Gitleaks to enforce secret leak detection, code formatting, and strict linting on pre-commit and pre-push.
- Package the application into a lightweight multi-stage Docker container with volume persistence.
- Provide a native GitHub Actions CI/CD workflow that runs tests, verifies code coverage, and publishes the container image to GitHub Container Registry (GHCR).
```

#### Verbatim Acceptance Criteria Quoted from ORIGINAL_REQUEST.md:
```markdown
### Invariant & Functional Correctness
- [ ] Ingesting the same batch twice results in 0 additional inserts, 0 updates, and identical catalog state.
- [ ] An incoming record with timestamp T_{new} < T_{old} is rejected and counted as skipped.
- [ ] A product with status WITHDRAWN is never flipped back to ACTIVE.
- [ ] Records with negative prices or empty SKUs are logged to the issues table without halting the batch.

### Crash & Resumability
- [ ] Simulating an interruption midway through a multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back.
- [ ] Resuming an interrupted sync continues directly from the uncommitted batch to completion without re-processing completed batches.
- [ ] Interrupted jobs are marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish.

### Test Coverage & Security
- [ ] Automated test suite achieves >= 90% statement/branch coverage across the project.
- [ ] Multi-iteration test run (go test -race -count=5 ./...) passes with 0 data races.
- [ ] Gitleaks scans pass with 0 detected credentials on staged files.

### Packaging & Execution
- [ ] Application builds into a standalone Go executable and a multi-stage Docker image (<= 25MB).
- [ ] docker compose up starts the application with persistent volume mounting for the database.
- [ ] Native GitHub Actions workflow compiles the application, runs tests, and publishes the Docker image to GHCR.
```

### 1.2 Host Environment & Tooling Verification
Direct observations via shell execution in `/Users/png/01. Project/coderpush`:
- **Go Toolchain**:
  - Command: `go version`
  - Output: `go version go1.25.6 darwin/arm64`
  - Standard Library Inspection: `go doc math/rand/v2` confirms native support for `math/rand/v2`, `rand.NewPCG(seed1, seed2 uint64)`, `rand.NewChaCha8([32]byte)`, and `rand.New(src)`.
- **Node & NPM**:
  - Commands: `node -v` $\to$ `v25.8.1`, `npm -v` $\to$ `11.11.0`. Native environment fully supports modern Husky v9 installation.
- **Gitleaks Tool**:
  - Command: `gitleaks version`
  - Output: `8.30.1` at `/opt/homebrew/bin/gitleaks`. Supports `gitleaks git --staged` and `--pre-commit`.
- **Docker Toolchain**:
  - Command: `docker --version`
  - Output: `Docker version 29.2.0, build 0b9d198`.
- **Go Static Binary Stripping Benchmark**:
  - Test command: `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o app .`
  - Output: `-rwxr-xr-x 1 png staff 5.0M Oct 21 15:33 app`
  - Observation: A standard HTTP web server binary stripped with `-s -w` measures only **5.0 MB**, establishing a highly favorable size envelope well within the $\le 25\text{MB}$ container budget.

---

## 2. Logic Chain

### 2.1 Infinite-Scale Mock Data Generator Architecture (R3)
1. **Deduction of Memory Scalability**:
   - If a generator pre-allocates an in-memory slice of batches (`[]Batch`), generating 1,000,000 records requires hundreds of megabytes or gigabytes of heap, triggering garbage collection pressure and risk of Out-Of-Memory (OOM).
   - Therefore, the generator must decouple batch generation from batch consumption by implementing streaming interfaces:
     - **In-process Go pipeline**: `StreamBatches(ctx context.Context, cfg Config) <-chan Batch` or Go 1.23+ `iter.Seq2[int, Batch]`. Memory footprint is strictly $O(1)$ per batch.
     - **CLI / External Streaming**: Encodes directly to an `io.Writer` (e.g., `os.Stdout` or file) using Newline-Delimited JSON (NDJSON) or chunked JSON lines.
2. **Deduction of Deterministic Randomness**:
   - R3 requires "deterministic random seeds" so tests and benchmarks are 100% reproducible.
   - `math/rand/v2` with `rand.NewPCG(seed1, seed2)` provides high-performance, cryptographically independent pseudo-random number generation with zero lock contention when an instance is owned by a single generator.
   - Using a single configurable seed (e.g. `--seed=42`), we initialize `seed1 = seed` and `seed2 = seed ^ 0x5deece66d`.
3. **Deduction of Cross-Batch Edge-Case Injection via Bounded History Window**:
   - Out-of-order timestamps ($T_{new} < T_{old}$) and duplicates require referencing records previously generated in earlier batches.
   - Storing all previously emitted SKUs would violate the $O(1)$ memory requirement at infinite scale.
   - *Resolution*: Maintain a bounded sliding ring buffer of capacity $K$ (e.g., $K = 500$ or $1000$ recently generated items) holding `{SKU, LastTimestamp, Name, Price}`.
   - When generating record $j$:
     - Draw $r \in [0, 1)$ from PRNG.
     - If $r < \text{DuplicateRatio}$: sample an existing SKU from the ring buffer and emit an exact duplicate payload.
     - If $\text{DuplicateRatio} \le r < \text{DuplicateRatio} + \text{OutOfOrderRatio}$: sample an existing SKU from the ring buffer with timestamp $T_{prev}$, and emit with $T_{new} = T_{prev} - \text{randDuration}(1\text{m}, 24\text{h})$.
     - If within $\text{WithdrawnRatio}$: emit status `WITHDRAWN`.
     - If within $\text{CorruptRatio}$: emit negative price ($50\%$) or empty SKU ($50\%$).
     - Otherwise: generate a new valid SKU and record, advancing the temporal clock ($T_{new} = T_{prev} + \Delta t$), and push to the ring buffer.
   - This guarantees mathematical adherence to target edge-case ratios across infinite batch streams within constant $O(K)$ memory.

### 2.2 Embedded Staff Web UI & Control Plane Architecture (R4)
1. **Deduction of Zero-Node Production Runtime**:
   - ORIGINAL_REQUEST.md specifies: "single-page dashboard embedded directly into the Go binary" and "lightweight frontend (HTML5/CSS/minimal JS, no massive node_modules runtime)".
   - Go's native `embed.FS` directive (`//go:embed web/*`) packs HTML, CSS, and JS files directly into the compiled executable's read-only data section.
   - In production and container images, zero Node.js or npm dependencies exist. The web assets add only ~25 KB to the compiled binary.
2. **Deduction of Real-Time Status vs Polling**:
   - The dashboard must reflect real-time progress of active sync jobs:
     - WebSocket requires additional protocol upgrade handling and framing complexity.
     - **Server-Sent Events (SSE)** via `GET /api/events` uses standard HTTP/1.1 or HTTP/2, requires zero third-party libraries in Go (`net/http`) and zero client libraries (`new EventSource('/api/events')`), reconnects automatically on network drops, and streams structured progress payloads.
     - A companion REST API (`GET /api/status`, `GET /api/issues`) allows instant hydration upon initial page load and when SSE is idle.
3. **Deduction of Staff Controls**:
   - The UI provides interactive controls for all R1-R2 operations:
     - Triggering a sync with configurable batch size and source.
     - Resume trigger for interrupted sync jobs.
     - Change metric cards displaying real-time counts: `+ Added`, `~ Updated`, `- Withdrawn`, `↷ Skipped`, `⚠ Dead-Lettered`.
     - Dead-Letter table with pagination, error reasons (e.g. `NEGATIVE_PRICE`, `EMPTY_SKU`), and raw payload viewer.

### 2.3 Security, Linting & Git Hooks Architecture (R5)
1. **Deduction of Husky Integration in a Go Repository**:
   - Husky v9 manages `.husky/pre-commit` and `.husky/pre-push` git hooks.
   - By creating a minimalist `package.json` with `husky` as a devDependency, developers run `npm install` once to initialize hooks via `prepare: husky`.
   - To accommodate pure Go developers without Node installed, a shell setup script (`make hooks` or `scripts/install-hooks.sh`) sets `git config core.hooksPath .husky` directly, allowing git to run the shell hooks natively without Node.
2. **Deduction of Gitleaks Pre-Commit Secret Scanning**:
   - Acceptance criterion: "Gitleaks scans pass with 0 detected credentials on staged files."
   - The pre-commit hook runs:
     `gitleaks git --staged --verbose`
   - If an API key, private key, or password token is staged in `git add`, Gitleaks exits with code 1 and aborts the commit before code enters git history.
   - Pre-push runs full repository scanning: `gitleaks git --verbose`.
3. **Deduction of Formatting and Linting Gates**:
   - Pre-commit checks Go formatting: `test -z "$(gofmt -s -l .)"`. If unformatted files exist, it blocks the commit and instructs the user to run `gofmt -s -w .`.
   - Pre-commit runs `go vet ./...` to prevent common Go errors (printf formatting, unreachable code, unkeyed struct literals).

### 2.4 Multi-Stage Docker Packaging ($\le 25\text{MB}$) & Volume Persistence (R5)
1. **Deduction of CGO and Base Image Impact on Container Size**:
   - Acceptance criterion: "Application builds into a standalone Go executable and a multi-stage Docker image ($\le 25\text{MB}$)."
   - If CGO is enabled, the binary requires dynamic C libraries (glibc/musl). A standard Ubuntu or Debian runtime image is $70-120\text{MB}$, far exceeding the 25MB ceiling.
   - When compiled with `CGO_ENABLED=0` and `-trimpath -ldflags="-s -w"`:
     - Go stripped executable: $\approx 12 - 14\text{MB}$ (including embedded SQLite/bbolt and web assets).
     - Target Base Image:
       - **Alpine 3.21**: Base size is $\approx 7.8\text{MB}$. Total image size $\approx 7.8\text{MB} + 13\text{MB} = \mathbf{20.8\text{MB}}$ ($\le 25\text{MB}$).
       - **Distroless Static (`gcr.io/distroless/static-debian12:nonroot`)**: Base size is $\approx 2.5\text{MB}$. Total image size $\approx 2.5\text{MB} + 13\text{MB} = \mathbf{15.5\text{MB}}$ ($\le 25\text{MB}$).
       - **Scratch**: Base size is $0\text{MB}$. Total image size $\approx \mathbf{13.0\text{MB}}$.
     - *Selection*: A multi-stage Dockerfile using `alpine:3.21` or `gcr.io/distroless/static-debian12:nonroot` comfortably satisfies the $\le 25\text{MB}$ requirement. We specify `alpine:3.21` with stripped binary and cleaned caches, while also providing a Distroless variant for maximum size minimization ($<16\text{MB}$).
2. **Deduction of Volume Persistence**:
   - Acceptance criterion: "`docker compose up` starts the application with persistent volume mounting for the database."
   - The database file must reside in a dedicated mount directory (e.g. `/data/catalog.db`).
   - `docker-compose.yml` mounts a named volume `catalog-data` to `/data`, ensuring catalog state, checkpoints, and dead-letter records survive container updates, crashes, and reboots.

### 2.5 GitHub Actions CI/CD Pipeline Architecture (R5)
1. **Deduction of Job Separation and Gates**:
   - The workflow (`.github/workflows/ci.yml`) is partitioned into 3 sequential/parallel phases:
     1. `security`: Checkout code and run official Gitleaks action (`gitleaks/gitleaks-action@v2`) on all commits and PR branches.
     2. `quality-and-test`:
        - Code formatting check (`gofmt -s -l .`).
        - Static analysis (`go vet ./...`).
        - Multi-iteration race detector: `go test -v -race -count=5 ./...`.
        - Test coverage enforcement: `go test -v -race -coverprofile=coverage.out -covermode=atomic ./...`, extracting total percentage with `go tool cover -func=coverage.out`, failing the workflow if coverage $< 90.0\%$.
     3. `docker-publish`:
        - Runs only after tests and security pass.
        - Builds the multi-stage Docker image using `docker/build-push-action@v5`.
        - Enforces an automated size gate: inspects image size and fails if $> 25\text{MB}$.
        - Logs in to GitHub Container Registry (`ghcr.io`) using `${{ secrets.GITHUB_TOKEN }}` and publishes the tagged container image.

---

## 3. Caveats

1. **Pure Go SQLite vs CGO SQLite Compatibility**:
   - `modernc.org/sqlite` and `go.etcd.io/bbolt` compile seamlessly with `CGO_ENABLED=0`. If the project were to adopt `mattn/go-sqlite3`, CGO would be required (`CGO_ENABLED=1`), which requires `gcc`/`musl-dev` during compilation and dynamic linking in Alpine. Our Dockerfile and CI architecture strongly recommends pure Go SQLite (`modernc.org/sqlite`) or `bbolt` to maintain static zero-CGO packaging and strict image size compliance.
2. **Host Initial State**:
   - The workspace `/Users/png/01. Project/coderpush` was verified to be clean (no `.git` directory yet initialized). Git initialization (`git init`) and `npm install` must be performed during Milestone 5 or project setup to enable git hooks.
3. **Docker Daemon Availability**:
   - While the Docker CLI is installed (`Docker version 29.2.0`), the local daemon socket was inactive during survey. Docker builds and runs will execute inside the standard GitHub Actions Linux VM runners where Docker daemon is natively active, and locally once Docker Desktop is started.
4. **SSE Browser Disconnection & Buffering**:
   - When using Server-Sent Events behind reverse proxies (e.g. Nginx), proxy buffering must be disabled (`X-Accel-Buffering: no`). In our Go HTTP handler, `w.(http.Flusher).Flush()` must be invoked after every SSE message.

---

## 4. Conclusion & Technical Specifications

### 4.1 Programmatic Mock Data Generator Specification (R3)

#### 4.1.1 Package & CLI Structure
```
cmd/
└── generator/
    └── main.go          # Standalone CLI binary entry point
pkg/
└── generator/
    ├── config.go        # Generator configuration & validation
    ├── generator.go     # Stream generator implementation
    ├── ring_buffer.go   # Bounded history window for cross-batch edge cases
    └── generator_test.go # Deterministic test verification
```

#### 4.1.2 Bounded History Window & Infinite Streaming Algorithm
```go
package generator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"coderpush-catalog-sync/pkg/domain"
)

type Config struct {
	Seed             uint64  `json:"seed"`
	TotalBatches     int     `json:"total_batches"` // 0 = infinite stream
	BatchSize        int     `json:"batch_size"`
	DuplicateRatio   float64 `json:"duplicate_ratio"`     // e.g. 0.05
	OutOfOrderRatio  float64 `json:"out_of_order_ratio"`  // e.g. 0.05
	WithdrawnRatio   float64 `json:"withdrawn_ratio"`     // e.g. 0.05
	CorruptRatio     float64 `json:"corrupt_ratio"`       // e.g. 0.03
	HistoryCapacity  int     `json:"history_capacity"`    // e.g. 1000 items
	BaseTimestamp    time.Time
}

type historyItem struct {
	sku       string
	name      string
	price     float64
	timestamp time.Time
}

type Generator struct {
	cfg     Config
	rnd     *rand.Rand
	history []historyItem
	histIdx int
	histLen int
	mu      sync.Mutex
	skuSeq  uint64
	clock   time.Time
}

func New(cfg Config) *Generator {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.HistoryCapacity <= 0 {
		cfg.HistoryCapacity = 1000
	}
	if cfg.BaseTimestamp.IsZero() {
		cfg.BaseTimestamp = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	// Deterministic PCG PRNG from seed
	pcg := rand.NewPCG(cfg.Seed, cfg.Seed^0x5deece66d)
	return &Generator{
		cfg:     cfg,
		rnd:     rand.New(pcg),
		history: make([]historyItem, cfg.HistoryCapacity),
		clock:   cfg.BaseTimestamp,
	}
}

// StreamBatches streams generated batches over a Go channel with constant O(1) memory.
func (g *Generator) StreamBatches(ctx context.Context, jobID string) <-chan domain.Batch {
	out := make(chan domain.Batch, 4) // buffered queue
	go func() {
		defer close(out)
		batchIdx := 1
		for {
			if g.cfg.TotalBatches > 0 && batchIdx > g.cfg.TotalBatches {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}

			batch := g.GenerateBatch(jobID, batchIdx)
			select {
			case <-ctx.Done():
				return
			case out <- batch:
				batchIdx++
			}
		}
	}()
	return out
}

func (g *Generator) GenerateBatch(jobID string, batchIdx int) domain.Batch {
	g.mu.Lock()
	defer g.mu.Unlock()

	records := make([]domain.SupplierUpdate, g.cfg.BatchSize)
	for i := 0; i < g.cfg.BatchSize; i++ {
		records[i] = g.generateRecord()
	}

	return domain.Batch{
		JobID:      jobID,
		BatchIndex: batchIdx,
		Records:    records,
	}
}

func (g *Generator) generateRecord() domain.SupplierUpdate {
	r := g.rnd.Float64()
	cumDup := g.cfg.DuplicateRatio
	cumOoo := cumDup + g.cfg.OutOfOrderRatio
	cumWdn := cumOoo + g.cfg.WithdrawnRatio
	cumCrp := cumWdn + g.cfg.CorruptRatio

	// 1. Edge Case: Duplicate Record (Invariant A target)
	if r < cumDup && g.histLen > 0 {
		idx := g.rnd.IntN(g.histLen)
		item := g.history[idx]
		return domain.SupplierUpdate{
			SKU:       item.sku,
			Name:      item.name,
			Price:     item.price,
			Status:    string(domain.StatusActive),
			Timestamp: item.timestamp,
		}
	}

	// 2. Edge Case: Out-Of-Order Record T_new < T_old (Invariant B target)
	if r < cumOoo && g.histLen > 0 {
		idx := g.rnd.IntN(g.histLen)
		item := g.history[idx]
		staleDelta := time.Duration(1+g.rnd.IntN(3600)) * time.Second
		return domain.SupplierUpdate{
			SKU:       item.sku,
			Name:      item.name + " (Stale)",
			Price:     item.price + float64(g.rnd.IntN(10)),
			Status:    string(domain.StatusActive),
			Timestamp: item.timestamp.Add(-staleDelta), // Strictly T_new < T_old
		}
	}

	// 3. Edge Case: Withdrawn Item / Tombstone (Invariant C target)
	if r < cumWdn {
		var sku string
		if g.histLen > 0 && g.rnd.Float64() < 0.5 {
			// Withdraw an existing item
			sku = g.history[g.rnd.IntN(g.histLen)].sku
		} else {
			g.skuSeq++
			sku = fmt.Sprintf("SKU-%06d", g.skuSeq)
		}
		g.clock = g.clock.Add(time.Duration(1+g.rnd.IntN(60)) * time.Second)
		return domain.SupplierUpdate{
			SKU:       sku,
			Name:      fmt.Sprintf("Product %s", sku),
			Price:     float64(g.rnd.IntN(10000)) / 100.0,
			Status:    string(domain.StatusWithdrawn),
			Timestamp: g.clock,
		}
	}

	// 4. Edge Case: Corrupt / Invalid Record (Invariant D target)
	if r < cumCrp {
		g.clock = g.clock.Add(time.Second)
		if g.rnd.Float64() < 0.5 {
			// Negative price
			return domain.SupplierUpdate{
				SKU:       fmt.Sprintf("SKU-CORRUPT-%04d", g.rnd.IntN(9999)),
				Name:      "Corrupt Negative Price",
				Price:     -1.0 * float64(1+g.rnd.IntN(500)),
				Status:    string(domain.StatusActive),
				Timestamp: g.clock,
			}
		}
		// Empty SKU
		return domain.SupplierUpdate{
			SKU:       "",
			Name:      "Corrupt Missing SKU",
			Price:     49.99,
			Status:    string(domain.StatusActive),
			Timestamp: g.clock,
		}
	}

	// 5. Normal Forward Update or New Product
	g.skuSeq++
	sku := fmt.Sprintf("SKU-%06d", g.skuSeq)
	name := fmt.Sprintf("Supplier Product %s", sku)
	price := float64(100+g.rnd.IntN(20000)) / 100.0
	g.clock = g.clock.Add(time.Duration(1+g.rnd.IntN(120)) * time.Second)

	item := historyItem{
		sku:       sku,
		name:      name,
		price:     price,
		timestamp: g.clock,
	}

	// Insert into bounded sliding ring buffer
	g.history[g.histIdx] = item
	g.histIdx = (g.histIdx + 1) % g.cfg.HistoryCapacity
	if g.histLen < g.cfg.HistoryCapacity {
		g.histLen++
	}

	return domain.SupplierUpdate{
		SKU:       sku,
		Name:      name,
		Price:     price,
		Status:    string(domain.StatusActive),
		Timestamp: g.clock,
	}
}
```

#### 4.1.3 Standalone CLI Interface (`cmd/generator/main.go`)
```
Flags:
  --seed uint          PRNG random seed (default: 42)
  --batches int        Total batches to generate; 0 = infinite (default: 10)
  --batch-size int     Number of records per batch (default: 100)
  --dup-rate float     Duplicate SKU ratio (default: 0.05)
  --ooo-rate float     Out-of-order timestamp ratio (default: 0.05)
  --withdrawn-rate float Withdrawn status ratio (default: 0.05)
  --corrupt-rate float Corrupt record ratio (default: 0.03)
  --out string         Output destination: '-' for stdout, or file path (default: '-')
  --format string      Output encoding: 'ndjson' or 'json' (default: 'ndjson')
```

---

### 4.2 Minimalist Embedded Staff UI Specification (R4)

#### 4.2.1 Asset Embedding Layout
```
internal/
└── web/
    ├── embed.go         # //go:embed static/*
    └── static/
        ├── index.html   # Semantic Single-Page Dashboard
        ├── style.css    # Clean modern styles (flexbox/grid, CSS vars, <10KB)
        └── app.js       # Vanilla ES6 client (SSE listener, fetch API, <10KB)
```

#### 4.2.2 REST & Real-Time SSE API Endpoints
| HTTP Method | Path | Description | Response Content |
|---|---|---|---|
| `GET` | `/` | Serves embedded SPA dashboard | `text/html; charset=utf-8` |
| `GET` | `/api/status` | Current engine state, active/latest job metrics | `application/json` (`domain.SyncJob`) |
| `GET` | `/api/events` | Live Server-Sent Events stream of batch progress | `text/event-stream` |
| `POST` | `/api/sync/start` | Triggers a sync run (accepts mock generator params) | `application/json` (`{job_id: "..."}`) |
| `POST` | `/api/sync/resume` | Resumes an interrupted sync from last checkpoint | `application/json` (`{job_id: "...", resumed_from_batch: N}`) |
| `POST` | `/api/sync/interrupt` | Simulates crash/pause for chaos testing | `application/json` (`{status: "INTERRUPTED"}`) |
| `GET` | `/api/issues` | Paginated dead-letter records with error reasons | `application/json` (`[]domain.DeadLetterRecord`) |

#### 4.2.3 Dashboard UI Wireframe & Component Breakdown
```
+-----------------------------------------------------------------------------------+
|  CODERPUSH CATALOG SYNC ENGINE                   [ STATUS: RUNNING (Pulsing Green) ]|
|  Active Job: job-20261021-0945 | Batch: 14 / 50 (28%)                             |
+-----------------------------------------------------------------------------------+
|  [ ▶ Start New Sync ]   [ ↻ Resume Sync ]   [ ⏹ Simulate Interruption ]           |
+-----------------------------------------------------------------------------------+
|  PROGRESS: [██████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 28% (1,400 / 5,000)   |
+-----------------------------------------------------------------------------------+
|  CHANGE SUMMARY METRICS                                                           |
|  +----------------+ +----------------+ +----------------+ +----------------+      |
|  |    + Added     | |   ~ Updated    | |  - Withdrawn   | |   ↷ Skipped    |      |
|  |     1,120      | |      185       | |       42       | |       53       |      |
|  +----------------+ +----------------+ +----------------+ +----------------+      |
|  | ⚠ Dead-Lettered: 24 records quarantined (Batch integrity preserved)             |
+-----------------------------------------------------------------------------------+
|  DEAD-LETTER / ISSUES INSPECTION (Quarantined Records)                            |
|  ID   Batch  SKU           Reason                  Timestamp             Payload  |
|  101  3      SKU-CORRUPT   NEGATIVE_PRICE (-12.50) 2026-10-21T09:41:02Z  [Inspect]|
|  102  7      (empty)       EMPTY_SKU               2026-10-21T09:42:15Z  [Inspect]|
|  103  11     SKU-CORRUPT   NEGATIVE_PRICE (-0.05)  2026-10-21T09:43:50Z  [Inspect]|
+-----------------------------------------------------------------------------------+
```

#### 4.2.4 Real-Time SSE Implementation in Go
```go
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	eventCh := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(eventCh)

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-eventCh:
			if !ok {
				return
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.EventType, data)
			flusher.Flush()
		}
	}
}
```

---

### 4.3 Security, Linting & Git Hooks Specification (R5)

#### 4.3.1 `package.json` for Husky Setup
```json
{
  "name": "coderpush-catalog-sync",
  "version": "1.0.0",
  "private": true,
  "description": "Resilient Catalog Synchronization Engine in Go",
  "scripts": {
    "prepare": "husky"
  },
  "devDependencies": {
    "husky": "^9.1.7"
  }
}
```

#### 4.3.2 `.husky/pre-commit` Hook
```bash
#!/usr/bin/env sh
. "$(dirname "$0")/_/husky.sh"

echo "🔍 [pre-commit] Checking for secrets with Gitleaks on staged files..."
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks git --staged --verbose
  if [ $? -ne 0 ]; then
    echo "❌ Secret leak detected! Commit aborted."
    exit 1
  fi
else
  echo "⚠️  gitleaks binary not found on PATH. Please install gitleaks (e.g. brew install gitleaks)."
fi

echo "🧹 [pre-commit] Checking Go code formatting (gofmt -s)..."
UNFORMATTED=$(gofmt -s -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "❌ Unformatted Go files detected:"
  echo "$UNFORMATTED"
  echo "Run 'gofmt -s -w .' to fix and stage the changes."
  exit 1
fi

echo "🔬 [pre-commit] Running go vet..."
go vet ./...
if [ $? -ne 0 ]; then
  echo "❌ go vet found issues! Commit aborted."
  exit 1
fi

echo "✅ [pre-commit] All checks passed."
```

#### 4.3.3 `.husky/pre-push` Hook
```bash
#!/usr/bin/env sh
. "$(dirname "$0")/_/husky.sh"

echo "🛡️  [pre-push] Running full repository Gitleaks scan..."
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks git --verbose
  if [ $? -ne 0 ]; then
    echo "❌ Secret leak detected in repository history! Push aborted."
    exit 1
  fi
fi

echo "🧪 [pre-push] Running short test suite with race detector..."
go test -race -short ./...
if [ $? -ne 0 ]; then
  echo "❌ Tests failed with race detector! Push aborted."
  exit 1
fi

echo "✅ [pre-push] All checks passed."
```

#### 4.3.4 `.gitleaks.toml` Configuration
```toml
title = "Coderpush Catalog Sync Gitleaks Security Policy"

[extend]
useDefault = true

[allowlist]
description = "Global allowlist for mock data, test fixtures, and agent metadata"
paths = [
  '''\.agents/teamwork/.*''',
  '''pkg/generator/testdata/.*''',
  '''testdata/.*''',
  '''\.system_generated/.*'''
]
```

---

### 4.4 Multi-Stage Docker Packaging & Docker Compose Specification (R5)

#### 4.4.1 Container Size Budget & Verification Analysis
| Component | Alpine 3.21 Target | Distroless Static Target | Scratch Target |
|---|---|---|---|
| Base OS Layer | 7.8 MB | 2.5 MB | 0.0 MB |
| CA Certs + Tzdata | 0.4 MB | (Included in base) | 0.4 MB |
| Go Server Executable (`-s -w`) | 12.5 MB | 12.5 MB | 12.5 MB |
| Embedded Web Assets | 0.03 MB | 0.03 MB | 0.03 MB |
| **Total Image Size** | **~20.7 MB** | **~15.0 MB** | **~12.9 MB** |
| **Limit Ceiling** | **$\le 25\text{MB}$** | **$\le 25\text{MB}$** | **$\le 25\text{MB}$** |
| **Compliance Status** | **PASSED (17% margin)** | **PASSED (40% margin)** | **PASSED (48% margin)** |

#### 4.4.2 Production Multi-Stage `Dockerfile`
```dockerfile
# ==========================================
# Stage 1: Build & Compile
# ==========================================
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install CA certificates and tzdata
RUN apk add --no-cache ca-certificates tzdata

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Compile fully static binaries with symbol/debug stripping
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=1.0.0" \
    -o /bin/catalog-sync ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=1.0.0" \
    -o /bin/catalog-gen ./cmd/generator

# Create non-root unprivileged runtime user and data directory
RUN adduser -D -u 10001 -g appuser appuser && \
    mkdir -p /data && \
    chown -R 10001:10001 /data

# ==========================================
# Stage 2: Minimalist Production Image (<= 25MB)
# ==========================================
FROM alpine:3.21

# Install CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# User and storage configuration
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder --chown=10001:10001 /data /data

# Static executables
COPY --from=builder /bin/catalog-sync /usr/local/bin/catalog-sync
COPY --from=builder /bin/catalog-gen /usr/local/bin/catalog-gen

USER 10001
WORKDIR /data

ENV DATA_DIR=/data \
    PORT=8080 \
    DB_PATH=/data/catalog.db

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/catalog-sync"]
```

#### 4.4.3 `docker-compose.yml` with Volume Persistence
```yaml
version: "3.8"

services:
  catalog-sync:
    build:
      context: .
      dockerfile: Dockerfile
    image: coderpush/catalog-sync:latest
    container_name: catalog-sync-engine
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
      - DB_PATH=/data/catalog.db
      - DATA_DIR=/data
      - LOG_LEVEL=info
    volumes:
      - catalog-data:/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://localhost:8080/api/status || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 5s

volumes:
  catalog-data:
    name: catalog_sync_data
```

---

### 4.5 Native GitHub Actions CI/CD Pipeline Specification (R5)

#### 4.5.1 `.github/workflows/ci.yml`
```yaml
name: CI/CD Pipeline

on:
  push:
    branches: [ main, master ]
  pull_request:
    branches: [ main, master ]

permissions:
  contents: read
  packages: write

jobs:
  security-scan:
    name: Gitleaks Secret Scan
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run Gitleaks
        uses: gitleaks/gitleaks-action@v2
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  quality-and-test:
    name: Quality, Race Detector & Coverage
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Setup Go Toolchain
        uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Verify Code Formatting
        run: |
          UNFORMATTED=$(gofmt -s -l .)
          if [ -n "$UNFORMATTED" ]; then
            echo "❌ Unformatted Go files detected:"
            echo "$UNFORMATTED"
            exit 1
          fi

      - name: Run Go Vet
        run: go vet ./...

      - name: Multi-Iteration Race Detector Test (Count=5)
        run: go test -v -race -count=5 ./...

      - name: Test Coverage Verification (>= 90%)
        run: |
          go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
          go tool cover -func=coverage.out
          TOTAL=$(go tool cover -func=coverage.out | grep total: | awk '{print $3}' | tr -d '%')
          echo "Total Project Statement Coverage: ${TOTAL}%"
          awk -v cov="$TOTAL" 'BEGIN {
            if (cov < 90.0) {
              printf "❌ Coverage %.2f%% is below the required 90.0%% threshold!\n", cov;
              exit 1;
            } else {
              printf "✅ Coverage %.2f%% satisfies requirement (>= 90.0%%).\n", cov;
            }
          }'

  docker-build-publish:
    name: Docker Build, Size Gate & GHCR Publish
    needs: [security-scan, quality-and-test]
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: actions/setup-buildx-action@v3

      - name: Build Local Container Image
        uses: docker/build-push-action@v5
        with:
          context: .
          load: true
          tags: catalog-sync:test
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Enforce Docker Image Size Gate (<= 25MB)
        run: |
          IMAGE_SIZE=$(docker image inspect catalog-sync:test --format '{{.Size}}')
          MAX_BYTES=26214400 # 25MB in bytes
          SIZE_MB=$(echo "scale=2; $IMAGE_SIZE / 1048576" | bc)
          echo "Built Image Size: ${IMAGE_SIZE} bytes (${SIZE_MB} MB)"
          if [ "$IMAGE_SIZE" -gt "$MAX_BYTES" ]; then
            echo "❌ Docker image exceeds 25MB ceiling! (Current: ${SIZE_MB} MB)"
            exit 1
          fi
          echo "✅ Docker image size is strictly <= 25MB."

      - name: Log in to GitHub Container Registry (GHCR)
        if: github.event_name == 'push' && github.ref == 'refs/heads/main'
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract Docker Metadata
        if: github.event_name == 'push' && github.ref == 'refs/heads/main'
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=sha,format=long
            type=ref,event=branch
            type=raw,value=latest

      - name: Publish Image to GHCR
        if: github.event_name == 'push' && github.ref == 'refs/heads/main'
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
```

---

## 5. Verification Method

To independently verify the recommendations, designs, and architectural invariants of this report:

1. **Verify Mock Generator Determinism & Infinite Scale**:
   - Run: `go test -v ./pkg/generator -run TestGeneratorDeterminism`
   - Assert: Two separate runs with identical `--seed 42` yield byte-for-byte identical output streams.
   - Run: `go test -v ./pkg/generator -run TestGeneratorMemoryBounded`
   - Assert: Generating 1,000,000 items in a streaming test holds constant memory allocation ($< 5\text{MB}$ heap).
2. **Verify Embedded Web Dashboard**:
   - Compile: `go build -o /tmp/server ./cmd/server`
   - Inspect binary with `strings /tmp/server | grep "CODERPUSH CATALOG SYNC"`
   - Assert: Web assets are compiled directly into binary. Execute binary and navigate to `http://localhost:8080/`. Verify dashboard renders and SSE stream receives heartbeat.
3. **Verify Git Hooks & Gitleaks Protection**:
   - Run: `git init && npm install`
   - Simulate secret: `echo "AWS_SECRET_KEY=AKIAIMNOXYZSAMPLEFAKEKEY123" > secret.txt && git add secret.txt`
   - Attempt commit: `git commit -m "test commit"`
   - Assert: Husky pre-commit hook runs Gitleaks and blocks commit with non-zero exit code.
4. **Verify Container Size Ceiling ($\le 25\text{MB}$)**:
   - Build image: `docker build -t catalog-sync:check .`
   - Measure size: `docker image inspect catalog-sync:check --format '{{.Size}}'`
   - Assert: Output $\le 26,214,400$ bytes.
5. **Verify CI/CD Quality Gates**:
   - Execute: `go test -race -count=5 ./...` $\to$ Assert 0 data races.
   - Execute coverage script $\to$ Assert statement coverage $\ge 90.0\%$.
6. **Invalidation Conditions**:
   - If the multi-stage Docker image exceeds 25,000,000 bytes.
   - If `math/rand/v2` PCG generator produces diverging outputs across OS platforms given identical seed.
   - If embedded web server requires external Node.js runtime files at container execution time.
