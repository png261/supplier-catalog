package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/generator"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// Handler serves REST API and control plane endpoints.
type Handler struct {
	storage       storage.Storage
	engine        catalogSync.Engine
	broker        *Broker
	mu            sync.Mutex
	activeJobID   string
	activeCancel  context.CancelFunc
	activeConfigs map[string]generator.Config
}

// NewHandler initializes a new API Handler.
func NewHandler(store storage.Storage, eng catalogSync.Engine, broker *Broker) *Handler {
	return &Handler{
		storage:       store,
		engine:        eng,
		broker:        broker,
		activeConfigs: make(map[string]generator.Config),
	}
}

// RegisterRoutes registers all API endpoints on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/jobs", h.handleStartJob)
	mux.HandleFunc("POST /api/jobs/{id}/resume", h.handleResumeJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", h.handleCancelJob)
	mux.HandleFunc("POST /api/database/reset", h.handleResetDatabase)
	mux.HandleFunc("GET /api/jobs", h.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", h.handleGetJob)
	mux.HandleFunc("GET /api/dead-letter", h.handleListDeadLetter)
	mux.HandleFunc("POST /api/dead-letter/{id}/resolve", h.handleResolveDeadLetter)
	mux.HandleFunc("GET /api/products", h.handleListProducts)
	mux.HandleFunc("GET /api/metrics", h.handleGetMetrics)
	mux.HandleFunc("GET /api/events", h.broker.ServeHTTP)
}

// Routes returns an http.Handler with all API routes registered.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// CancelActiveJob cancels any currently active synchronization job.
func (h *Handler) CancelActiveJob() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activeCancel != nil {
		h.activeCancel()
		h.activeCancel = nil
	}
}

// handleCancelJob handles POST /api/jobs/{id}/cancel.
func (h *Handler) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "job ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	h.mu.Lock()
	if h.activeJobID != jobID || h.activeCancel == nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("job %s is not currently running or active", jobID), ErrCodeJobNotFound)
		return
	}
	cancel := h.activeCancel
	h.mu.Unlock()

	cancel()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":  "cancelling",
		"job_id":  jobID,
		"message": "cancellation signal sent successfully",
	})
}

// handleResetDatabase handles POST /api/database/reset.
func (h *Handler) handleResetDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h.mu.Lock()
	if h.activeJobID != "" {
		activeID := h.activeJobID
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("cannot reset database while job %s is running", activeID), ErrCodeJobConflict)
		return
	}
	h.mu.Unlock()

	if err := h.storage.Reset(ctx); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to reset database: "+err.Error(), ErrCodeInternalError)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "database reset to clean state",
	})
}

// writeJSON writes a JSON response with status code.
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes a standardized error JSON response.
func (h *Handler) writeError(w http.ResponseWriter, status int, message string, code ErrorCode) {
	h.writeJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}

// handleStartJob handles POST /api/jobs.
func (h *Handler) handleStartJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Parse request body and validate inputs prior to locking
	var req JobStartRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
			return
		}
	}

	if req.Mode == "" {
		req.Mode = "generator"
	}

	jobID := req.JobID
	if jobID == "" {
		jobID = fmt.Sprintf("job-%d", time.Now().UnixNano())
	}

	var totalBatches int
	var directBatches []domain.Batch
	cfg := generator.DefaultConfig()

	if req.Mode == "batches" {
		if len(req.Batches) == 0 {
			h.writeError(w, http.StatusBadRequest, "batches array cannot be empty in batches mode", ErrCodeInvalidRequest)
			return
		}
		totalBatches = len(req.Batches)
		for _, bInput := range req.Batches {
			batch := domain.Batch{
				JobID:      jobID,
				BatchIndex: bInput.BatchIndex,
				Records:    make([]domain.SupplierUpdate, 0, len(bInput.Records)),
			}
			for _, rec := range bInput.Records {
				ts := time.Now().UTC()
				if rec.Timestamp != "" {
					parsed, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
					if err != nil {
						parsed, err = time.Parse(time.RFC3339, rec.Timestamp)
					}
					if err != nil {
						h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid timestamp '%s': %v", rec.Timestamp, err), ErrCodeInvalidRequest)
						return
					}
					ts = parsed.UTC()
				}
				batch.Records = append(batch.Records, domain.SupplierUpdate{
					SKU:       rec.SKU,
					Name:      rec.Name,
					Price:     rec.Price,
					Status:    rec.Status,
					Timestamp: ts,
				})
			}
			directBatches = append(directBatches, batch)
		}
	} else if req.Mode == "generator" {
		if req.Seed != nil {
			cfg.Seed = *req.Seed
		}
		if req.TotalBatches != nil {
			cfg.TotalBatches = *req.TotalBatches
		}
		if req.BatchSize != nil {
			cfg.BatchSize = *req.BatchSize
		}
		if req.UpdateRatio != nil {
			cfg.UpdateRatio = *req.UpdateRatio
		}
		if req.DuplicateRatio != nil {
			cfg.DuplicateRatio = *req.DuplicateRatio
		}
		if req.OutOfOrderRatio != nil {
			cfg.OutOfOrderRatio = *req.OutOfOrderRatio
		}
		if req.WithdrawnRatio != nil {
			cfg.WithdrawnRatio = *req.WithdrawnRatio
		}
		if req.InvalidRatio != nil {
			cfg.InvalidRatio = *req.InvalidRatio
		}

		// Validate ratio ranges
		ratios := []float64{cfg.UpdateRatio, cfg.DuplicateRatio, cfg.OutOfOrderRatio, cfg.WithdrawnRatio, cfg.InvalidRatio}
		for _, rVal := range ratios {
			if rVal < 0.0 || rVal > 1.0 {
				h.writeError(w, http.StatusBadRequest, "ratio parameters must be between 0.0 and 1.0", ErrCodeInvalidRequest)
				return
			}
		}

		// Validate sum of ratios <= 1.0
		sumRatios := cfg.UpdateRatio + cfg.DuplicateRatio + cfg.OutOfOrderRatio + cfg.WithdrawnRatio + cfg.InvalidRatio
		if sumRatios > 1.0000001 { // allow tiny float tolerance
			h.writeError(w, http.StatusBadRequest, "sum of anomaly and update ratios cannot exceed 1.0", ErrCodeInvalidRequest)
			return
		}

		if err := cfg.Validate(); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid generator config: "+err.Error(), ErrCodeInvalidRequest)
			return
		}

		totalBatches = cfg.TotalBatches
	} else {
		h.writeError(w, http.StatusBadRequest, "unsupported mode: "+req.Mode, ErrCodeInvalidRequest)
		return
	}

	// 2. Synchronized Active Job Verification, Storage Creation & State Transition
	h.mu.Lock()

	// Check 2a: In-memory active job (fast path)
	if h.activeJobID != "" {
		activeID := h.activeJobID
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", activeID), ErrCodeJobConflict)
		return
	}

	// Check 2b: Persistent storage active job
	active, err := h.storage.GetActiveJob(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
		h.mu.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed to check active jobs: "+err.Error(), ErrCodeInternalError)
		return
	}
	if active != nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", active.JobID), ErrCodeJobConflict)
		return
	}

	// 3. Atomically create initial job state in storage while holding lock
	now := time.Now().UTC()
	job := &domain.SyncJob{
		JobID:               jobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        totalBatches,
		CommittedBatchIndex: -1,
		Summary:             domain.ChangeSummary{},
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed to create sync job: "+err.Error(), ErrCodeInternalError)
		return
	}

	// 4. Mark job active in memory while holding lock
	bgCtx, cancel := context.WithCancel(context.Background())
	h.activeJobID = jobID
	h.activeCancel = cancel
	if req.Mode == "generator" {
		h.activeConfigs[jobID] = cfg
	}
	h.mu.Unlock()

	// 5. Return 202 Accepted
	h.writeJSON(w, http.StatusAccepted, SyncJobResponse{
		JobID:               job.JobID,
		Status:              job.Status,
		TotalBatches:        job.TotalBatches,
		CommittedBatchIndex: job.CommittedBatchIndex,
		Summary:             job.Summary,
		CreatedAt:           job.CreatedAt,
		UpdatedAt:           job.UpdatedAt,
	})

	// 6. Broadcast job_started event
	h.broker.Publish(Event{
		Type:  "job_started",
		JobID: jobID,
		Data: map[string]any{
			"job_id":        jobID,
			"status":        string(domain.JobStatusRunning),
			"total_batches": totalBatches,
			"created_at":    now,
		},
	})

	// 7. Spawn background job execution

	go func() {
		defer cancel()
		defer func() {
			h.mu.Lock()
			if h.activeJobID == jobID {
				h.activeJobID = ""
				h.activeCancel = nil
			}
			h.mu.Unlock()
		}()

		var stream <-chan domain.Batch
		if req.Mode == "batches" {
			ch := make(chan domain.Batch, len(directBatches))
			for _, b := range directBatches {
				ch <- b
			}
			close(ch)
			stream = ch
		} else {
			gen, err := generator.New(cfg)
			if err != nil {
				return
			}
			stream = gen.StreamBatches(bgCtx, jobID)
		}

		finalJob, err := h.engine.RunJob(bgCtx, jobID, stream)
		if err != nil {
			var committedIdx int = -1
			var sum domain.ChangeSummary
			if finalJob != nil {
				committedIdx = finalJob.CommittedBatchIndex
				sum = finalJob.Summary
			}
			h.broker.Publish(Event{
				Type:  "job_interrupted",
				JobID: jobID,
				Data: map[string]any{
					"job_id":                jobID,
					"status":                string(domain.JobStatusInterrupted),
					"committed_batch_index": committedIdx,
					"total_batches":         totalBatches,
					"summary":               sum,
					"reason":                err.Error(),
				},
			})
			return
		}

		h.broker.Publish(Event{
			Type:  "job_completed",
			JobID: jobID,
			Data: map[string]any{
				"job_id":        jobID,
				"status":        string(domain.JobStatusCompleted),
				"total_batches": finalJob.TotalBatches,
				"summary":       finalJob.Summary,
				"completed_at":  finalJob.CompletedAt,
			},
		})
	}()
}

// handleResumeJob handles POST /api/jobs/{id}/resume.
func (h *Handler) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := r.PathValue("id")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "job ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	// 1. Parse optional resume config BEFORE locking
	var resumeReq JobResumeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&resumeReq); err != nil {
			h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
			return
		}
	}

	// 2. Synchronized Active Job Verification, State Check & Status Transition
	h.mu.Lock()

	// Check 2a: In-memory active job (fast path)
	if h.activeJobID != "" {
		activeID := h.activeJobID
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", activeID), ErrCodeJobConflict)
		return
	}

	// Check 2b: Persistent storage active job
	active, err := h.storage.GetActiveJob(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
		h.mu.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed checking active jobs: "+err.Error(), ErrCodeInternalError)
		return
	}
	if active != nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", active.JobID), ErrCodeJobConflict)
		return
	}

	// Check 2c: Fetch target job
	job, err := h.storage.GetJob(ctx, jobID)
	if err != nil {
		h.mu.Unlock()
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "sync job not found: "+jobID, ErrCodeJobNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to lookup job: "+err.Error(), ErrCodeInternalError)
		return
	}

	// Check 2d: Verify target job state
	if job.Status == domain.JobStatusCompleted {
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, "job is already completed and cannot be resumed", ErrCodeJobConflict)
		return
	}
	if job.Status == domain.JobStatusRunning {
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", jobID), ErrCodeJobConflict)
		return
	}

	// 3. Resolve and validate generator config for resumption
	cfg, ok := h.activeConfigs[jobID]
	if !ok {
		cfg = generator.DefaultConfig()
		cfg.TotalBatches = job.TotalBatches
	}
	if resumeReq.Seed != nil {
		cfg.Seed = *resumeReq.Seed
	}
	if resumeReq.TotalBatches != nil {
		cfg.TotalBatches = *resumeReq.TotalBatches
	}
	if resumeReq.BatchSize != nil {
		cfg.BatchSize = *resumeReq.BatchSize
	}

	if err := cfg.Validate(); err != nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusBadRequest, "invalid resume config: "+err.Error(), ErrCodeInvalidRequest)
		return
	}

	// 4. CRITICAL: Atomically transition job status to RUNNING in storage while holding lock
	if err := h.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed to update job status: "+err.Error(), ErrCodeInternalError)
		return
	}

	// 5. Mark job active in memory while holding lock
	bgCtx, cancel := context.WithCancel(context.Background())
	h.activeJobID = jobID
	h.activeCancel = cancel
	h.activeConfigs[jobID] = cfg
	h.mu.Unlock()

	// 6. Respond 202 Accepted
	now := time.Now().UTC()
	h.writeJSON(w, http.StatusAccepted, SyncJobResponse{
		JobID:               job.JobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        cfg.TotalBatches,
		CommittedBatchIndex: job.CommittedBatchIndex,
		Summary:             job.Summary,
		CreatedAt:           job.CreatedAt,
		UpdatedAt:           now,
	})

	// 7. Broadcast job_started event for resumption
	h.broker.Publish(Event{
		Type:  "job_started",
		JobID: jobID,
		Data: map[string]any{
			"job_id":             jobID,
			"status":             string(domain.JobStatusRunning),
			"resumed_from_batch": job.CommittedBatchIndex + 1,
			"total_batches":      cfg.TotalBatches,
			"timestamp":          now,
		},
	})

	// 8. Spawn resume in background

	go func() {
		defer cancel()
		defer func() {
			h.mu.Lock()
			if h.activeJobID == jobID {
				h.activeJobID = ""
				h.activeCancel = nil
			}
			h.mu.Unlock()
		}()

		gen, err := generator.New(cfg)
		if err != nil {
			return
		}
		stream := gen.StreamBatches(bgCtx, jobID)

		finalJob, err := h.engine.ResumeJob(bgCtx, jobID, stream)
		if err != nil {
			var committedIdx int = -1
			var sum domain.ChangeSummary
			if finalJob != nil {
				committedIdx = finalJob.CommittedBatchIndex
				sum = finalJob.Summary
			}
			h.broker.Publish(Event{
				Type:  "job_interrupted",
				JobID: jobID,
				Data: map[string]any{
					"job_id":                jobID,
					"status":                string(domain.JobStatusInterrupted),
					"committed_batch_index": committedIdx,
					"total_batches":         cfg.TotalBatches,
					"summary":               sum,
					"reason":                err.Error(),
				},
			})
			return
		}

		h.broker.Publish(Event{
			Type:  "job_completed",
			JobID: jobID,
			Data: map[string]any{
				"job_id":        jobID,
				"status":        string(domain.JobStatusCompleted),
				"total_batches": finalJob.TotalBatches,
				"summary":       finalJob.Summary,
				"completed_at":  finalJob.CompletedAt,
			},
		})
	}()
}

// handleListJobs handles GET /api/jobs.
func (h *Handler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := 20
	if lStr := q.Get("limit"); lStr != "" {
		val, err := strconv.Atoi(lStr)
		if err != nil || val < 1 {
			h.writeError(w, http.StatusBadRequest, "invalid limit parameter", ErrCodeInvalidRequest)
			return
		}
		limit = val
	}

	offset := 0
	if oStr := q.Get("offset"); oStr != "" {
		val, err := strconv.Atoi(oStr)
		if err != nil || val < 0 {
			h.writeError(w, http.StatusBadRequest, "invalid offset parameter", ErrCodeInvalidRequest)
			return
		}
		offset = val
	}

	statusFilter := q.Get("status")
	if statusFilter != "" {
		validStatuses := map[string]bool{
			string(domain.JobStatusPending):     true,
			string(domain.JobStatusRunning):     true,
			string(domain.JobStatusInterrupted): true,
			string(domain.JobStatusCompleted):   true,
			string(domain.JobStatusFailed):      true,
		}
		if !validStatuses[statusFilter] {
			h.writeError(w, http.StatusBadRequest, "invalid status filter: "+statusFilter, ErrCodeInvalidRequest)
			return
		}
	}

	jobs, total, err := h.storage.ListJobs(ctx, limit, offset)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to query sync jobs: "+err.Error(), ErrCodeInternalError)
		return
	}

	if statusFilter != "" {
		filtered := make([]domain.SyncJob, 0)
		for _, j := range jobs {
			if string(j.Status) == statusFilter {
				filtered = append(filtered, j)
			}
		}
		jobs = filtered
		total = len(filtered)
	}

	if jobs == nil {
		jobs = []domain.SyncJob{}
	}

	h.writeJSON(w, http.StatusOK, JobListResponse{
		Jobs:   jobs,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// handleGetJob handles GET /api/jobs/{id}.
func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := r.PathValue("id")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "job ID is required", ErrCodeInvalidRequest)
		return
	}

	job, err := h.storage.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "sync job not found: "+jobID, ErrCodeJobNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve job: "+err.Error(), ErrCodeInternalError)
		return
	}

	checkpoints, err := h.storage.GetBatchCheckpoints(ctx, jobID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve checkpoints: "+err.Error(), ErrCodeInternalError)
		return
	}
	if checkpoints == nil {
		checkpoints = []domain.BatchCheckpoint{}
	}

	h.writeJSON(w, http.StatusOK, JobDetailResponse{
		Job:         job,
		Checkpoints: checkpoints,
	})
}

// handleListDeadLetter handles GET /api/dead-letter.
func (h *Handler) handleListDeadLetter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := 20
	if lStr := q.Get("limit"); lStr != "" {
		val, err := strconv.Atoi(lStr)
		if err != nil || val < 1 {
			h.writeError(w, http.StatusBadRequest, "invalid limit parameter", ErrCodeInvalidRequest)
			return
		}
		limit = val
	}

	offset := 0
	if oStr := q.Get("offset"); oStr != "" {
		val, err := strconv.Atoi(oStr)
		if err != nil || val < 0 {
			h.writeError(w, http.StatusBadRequest, "invalid offset parameter", ErrCodeInvalidRequest)
			return
		}
		offset = val
	}

	jobID := strings.TrimSpace(q.Get("job_id"))
	reason := strings.TrimSpace(q.Get("reason"))

	issues, total, err := h.storage.ListDeadLetterRecords(ctx, jobID, reason, limit, offset)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list dead-letter records: "+err.Error(), ErrCodeInternalError)
		return
	}
	if issues == nil {
		issues = []domain.DeadLetterRecord{}
	}

	h.writeJSON(w, http.StatusOK, DeadLetterListResponse{
		Issues: issues,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// ResolveDeadLetterRequest defines the payload for correcting a quarantined dead-letter product.
type ResolveDeadLetterRequest struct {
	SKU       string  `json:"sku"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Status    string  `json:"status"`
	Timestamp string  `json:"timestamp"`
}

// handleResolveDeadLetter handles POST /api/dead-letter/{id}/resolve.
func (h *Handler) handleResolveDeadLetter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	issueID := r.PathValue("id")
	if strings.TrimSpace(issueID) == "" {
		h.writeError(w, http.StatusBadRequest, "issue ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	var req ResolveDeadLetterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
		return
	}

	sku := strings.TrimSpace(req.SKU)
	if sku == "" {
		h.writeError(w, http.StatusBadRequest, "sku cannot be empty", ErrCodeInvalidRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Remediated Product"
	}

	if req.Price < 0 || math.IsNaN(req.Price) || math.IsInf(req.Price, 0) {
		h.writeError(w, http.StatusBadRequest, "price must be a finite non-negative number", ErrCodeInvalidRequest)
		return
	}

	status := domain.ProductStatus(strings.TrimSpace(req.Status))
	if !status.IsValid() {
		h.writeError(w, http.StatusBadRequest, "status must be ACTIVE or WITHDRAWN", ErrCodeInvalidRequest)
		return
	}

	var ts time.Time
	if strings.TrimSpace(req.Timestamp) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, req.Timestamp)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, req.Timestamp)
		}
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid timestamp format: "+err.Error(), ErrCodeInvalidRequest)
			return
		}
		ts = parsed.UTC()
	} else {
		ts = time.Now().UTC()
	}

	prod := &domain.Product{
		SKU:               sku,
		Name:              name,
		Price:             req.Price,
		Status:            status,
		SupplierTimestamp: ts,
		UpdatedAt:         time.Now().UTC(),
	}

	if err := h.storage.ResolveDeadLetter(ctx, issueID, prod); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "dead letter issue not found", ErrCodeNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to resolve dead letter record: "+err.Error(), ErrCodeInternalError)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "product resolved and imported to catalog",
		"product": prod,
	})
}

// handleListProducts handles GET /api/products.
func (h *Handler) handleListProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := 20
	if lStr := q.Get("limit"); lStr != "" {
		val, err := strconv.Atoi(lStr)
		if err != nil || val < 1 {
			h.writeError(w, http.StatusBadRequest, "invalid limit parameter", ErrCodeInvalidRequest)
			return
		}
		limit = val
	}

	offset := 0
	if oStr := q.Get("offset"); oStr != "" {
		val, err := strconv.Atoi(oStr)
		if err != nil || val < 0 {
			h.writeError(w, http.StatusBadRequest, "invalid offset parameter", ErrCodeInvalidRequest)
			return
		}
		offset = val
	}

	status := q.Get("status")
	if status != "" && status != string(domain.StatusActive) && status != string(domain.StatusWithdrawn) {
		h.writeError(w, http.StatusBadRequest, "invalid status parameter: "+status, ErrCodeInvalidRequest)
		return
	}

	search := q.Get("search")

	products, total, err := h.storage.ListProducts(ctx, status, search, limit, offset)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list products: "+err.Error(), ErrCodeInternalError)
		return
	}
	if products == nil {
		products = []domain.Product{}
	}

	h.writeJSON(w, http.StatusOK, ProductListResponse{
		Products: products,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// handleGetMetrics handles GET /api/metrics.
func (h *Handler) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	timeRange := strings.TrimSpace(strings.ToLower(q.Get("time_range")))
	jobID := strings.TrimSpace(q.Get("job_id"))
	fromStr := strings.TrimSpace(q.Get("from"))
	toStr := strings.TrimSpace(q.Get("to"))

	if timeRange == "" && jobID == "" && fromStr == "" {
		timeRange = "today"
	}

	filter := domain.MetricsFilter{
		TimeRange: timeRange,
		JobID:     jobID,
	}

	// Parse custom from / to timestamps if supplied
	if fromStr != "" {
		t, err := parseFlexTime(fromStr, false)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid from parameter: must be RFC3339 or YYYY-MM-DD", ErrCodeInvalidRequest)
			return
		}
		filter.From = &t
	}
	if toStr != "" {
		t, err := parseFlexTime(toStr, true)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid to parameter: must be RFC3339 or YYYY-MM-DD", ErrCodeInvalidRequest)
			return
		}
		filter.To = &t
	}

	// If no explicit from/to supplied and no specific job_id, derive from preset
	if jobID == "" && filter.From == nil && filter.To == nil {
		now := time.Now().UTC()
		switch timeRange {
		case "today":
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			end := start.Add(24*time.Hour - time.Nanosecond)
			filter.From = &start
			filter.To = &end
		case "yesterday":
			end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)
			start := end.Add(-24*time.Hour + time.Nanosecond)
			filter.From = &start
			filter.To = &end
		case "7d", "last7days":
			start := now.Add(-7 * 24 * time.Hour)
			filter.From = &start
			filter.To = &now
		case "all":
			// No bounds
		default:
			if timeRange != "custom" {
				h.writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported time_range '%s': must be today, yesterday, 7d, all, or custom", timeRange), ErrCodeInvalidRequest)
				return
			}
		}
	}

	agg, jobs, err := h.storage.GetMetrics(ctx, filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve metrics: "+err.Error(), ErrCodeInternalError)
		return
	}
	if jobs == nil {
		jobs = []domain.SyncJob{}
	}

	h.writeJSON(w, http.StatusOK, MetricsResponse{
		Filter:   filter,
		Metrics:  *agg,
		SyncJobs: jobs,
	})
}

// parseFlexTime parses either RFC3339, RFC3339Nano, or YYYY-MM-DD date strings.
func parseFlexTime(s string, isEndOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if d, err := time.Parse("2006-01-02", s); err == nil {
		if isEndOfDay {
			return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 999999999, time.UTC), nil
		}
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, errors.New("cannot parse time string")
}
