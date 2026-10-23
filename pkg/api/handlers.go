package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// HandlerOption configures Handler behavior.
type HandlerOption func(*Handler)

// WithHandlerDataDir configures the persistent base data directory for multi-tenant shop databases.
func WithHandlerDataDir(dataDir string) HandlerOption {
	return func(h *Handler) {
		if h.tenantManager != nil {
			h.tenantManager.baseDir = dataDir
		}
	}
}

// Handler serves REST API and control plane endpoints.
type Handler struct {
	storage       storage.Storage
	engine        catalogSync.Engine
	broker        *Broker
	mu            sync.Mutex
	activeJobID   string
	activeCancel  context.CancelFunc
	activeConfigs map[string]generator.Config
	tenantManager *TenantManager
}

// Ensure Handler implements tenantOps for the default tenant.
var _ tenantOps = (*Handler)(nil)

func (h *Handler) GetID() string                             { return "default" }
func (h *Handler) GetStorage() storage.Storage               { return h.storage }
func (h *Handler) GetEngine() catalogSync.Engine             { return h.engine }
func (h *Handler) GetBroker() *Broker                        { return h.broker }
func (h *Handler) Lock()                                     { h.mu.Lock() }
func (h *Handler) Unlock()                                   { h.mu.Unlock() }
func (h *Handler) GetActiveJobID() string                    { return h.activeJobID }
func (h *Handler) SetActiveJobID(id string)                  { h.activeJobID = id }
func (h *Handler) GetActiveCancel() context.CancelFunc       { return h.activeCancel }
func (h *Handler) SetActiveCancel(cancel context.CancelFunc) { h.activeCancel = cancel }

func (h *Handler) GetActiveConfig(jobID string) (generator.Config, bool) {
	if h.activeConfigs == nil {
		return generator.Config{}, false
	}
	cfg, ok := h.activeConfigs[jobID]
	return cfg, ok
}

func (h *Handler) SetActiveConfig(jobID string, cfg generator.Config) {
	if h.activeConfigs == nil {
		h.activeConfigs = make(map[string]generator.Config)
	}
	h.activeConfigs[jobID] = cfg
}

func (h *Handler) RemoveActiveConfig(jobID string) {
	if h.activeConfigs != nil {
		delete(h.activeConfigs, jobID)
	}
}

func (h *Handler) ResetActive() {
	h.activeJobID = ""
	h.activeCancel = nil
}

// resolveTenant resolves either the default tenant or the dynamic shop tenant from HTTP request.
func (h *Handler) resolveTenant(r *http.Request) (tenantOps, error) {
	shopID := ExtractShopID(r)
	if shopID == "" || shopID == "default" {
		return h, nil
	}
	if h.tenantManager == nil {
		return h, nil
	}
	return h.tenantManager.GetOrCreate(shopID)
}

// NewHandler initializes a new API Handler.
func NewHandler(store storage.Storage, eng catalogSync.Engine, broker *Broker, opts ...HandlerOption) *Handler {
	h := &Handler{
		storage:       store,
		engine:        eng,
		broker:        broker,
		activeConfigs: make(map[string]generator.Config),
	}
	h.tenantManager = NewTenantManager(".", h)
	for _, opt := range opts {
		opt(h)
	}
	return h
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
	mux.HandleFunc("GET /api/events", h.handleEvents)

	// Catalog Versioning & Patch Management (Milestone 4)
	mux.HandleFunc("POST /api/patches/preview", h.handlePreviewPatch)
	mux.HandleFunc("POST /api/patches/publish", h.handlePublishPatch)
	mux.HandleFunc("GET /api/versions", h.handleListVersions)
	mux.HandleFunc("GET /api/versions/{id}/diff", h.handleGetVersionDiff)
	mux.HandleFunc("POST /api/versions/{id}/rollback", h.handleRollbackVersion)
}

// @Summary Stream real-time synchronization events (SSE)
// @Description Establishes a persistent Server-Sent Events (SSE) connection streaming real-time job progress, status changes, dead-letter notifications, and catalog version events (version_created, version_rollback).
// @Tags Events
// @Produce text/event-stream
// @Param X-Shop-Id header string false "Isolated shop tenant ID (default: 'default')"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {string} string "Server-Sent Events text stream"
// @Failure 400 {object} ErrorResponse "Invalid shop ID"
// @Failure 500 {object} ErrorResponse "Streaming unsupported (client connection is non-flusher)"
// @Failure 503 {object} ErrorResponse "SSE Broker closed"
// @Router /api/events [get]
func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s","code":"INVALID_SHOP_ID"}`, err.Error()), http.StatusBadRequest)
		return
	}
	tenant.GetBroker().ServeHTTP(w, r)
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

// @Summary Cancel active synchronization job
// @Description Sends a cancellation signal to stop a currently running synchronization job.
// @Tags Jobs
// @Produce json
// @Param id path string true "Job ID to cancel"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} JobCancelResponse "Cancellation signal sent successfully"
// @Failure 400 {object} ErrorResponse "Missing job ID or invalid request"
// @Failure 404 {object} ErrorResponse "Job not currently running or active"
// @Router /api/jobs/{id}/cancel [post]
func (h *Handler) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "job ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	tenant.Lock()
	if tenant.GetActiveJobID() != jobID || tenant.GetActiveCancel() == nil {
		tenant.Unlock()
		// Fallback: if job is orphaned in RUNNING/PENDING state in storage without an active in-memory runner, mark INTERRUPTED
		if job, getErr := tenant.GetStorage().GetJob(r.Context(), jobID); getErr == nil && job != nil &&
			(job.Status == domain.JobStatusRunning || job.Status == domain.JobStatusPending) {
			_ = tenant.GetStorage().UpdateJobStatus(r.Context(), jobID, domain.JobStatusInterrupted)
			tenant.GetBroker().Publish(Event{
				Type:  "job_interrupted",
				JobID: jobID,
				Data: map[string]any{
					"job_id":                jobID,
					"status":                string(domain.JobStatusInterrupted),
					"committed_batch_index": job.CommittedBatchIndex,
					"total_batches":         job.TotalBatches,
					"summary":               job.Summary,
					"reason":                "cancelled by user request",
				},
			})
			h.writeJSON(w, http.StatusOK, JobCancelResponse{
				Status:  "cancelling",
				JobID:   jobID,
				Message: "cancellation signal sent successfully",
			})
			return
		}
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("job %s is not currently running or active", jobID), ErrCodeJobNotFound)
		return
	}
	cancel := tenant.GetActiveCancel()
	tenant.Unlock()

	cancel()

	h.writeJSON(w, http.StatusOK, JobCancelResponse{
		Status:  "cancelling",
		JobID:   jobID,
		Message: "cancellation signal sent successfully",
	})
}

// @Summary Reset catalog database
// @Description Clears all products, jobs, checkpoints, and dead-letter issues for the specified tenant. Blocked if a job is active.
// @Tags Database
// @Produce json
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} DatabaseResetResponse "Database reset to clean state"
// @Failure 400 {object} ErrorResponse "Invalid request or shop ID"
// @Failure 409 {object} ErrorResponse "Cannot reset database while a job is running"
// @Failure 500 {object} ErrorResponse "Failed to reset database"
// @Router /api/database/reset [post]
func (h *Handler) handleResetDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	tenant.Lock()
	if tenant.GetActiveJobID() != "" {
		activeID := tenant.GetActiveJobID()
		tenant.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("cannot reset database while job %s is running", activeID), ErrCodeJobConflict)
		return
	}
	tenant.Unlock()

	if err := tenant.GetStorage().Reset(ctx); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to reset database: "+err.Error(), ErrCodeInternalError)
		return
	}

	h.writeJSON(w, http.StatusOK, DatabaseResetResponse{
		Status:  "success",
		Message: "database reset to clean state",
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

// @Summary Start synchronization job
// @Description Initiates a catalog synchronization job in background. Supports synthetic data generation ("generator") or direct batch ingestion ("batches").
// @Tags Jobs
// @Accept json
// @Produce json
// @Param X-Shop-Id header string false "Isolated shop tenant ID (default: 'default')"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Param request body JobStartRequest false "Synchronization job configuration and payload"
// @Success 202 {object} SyncJobResponse "Synchronization job accepted and running in background"
// @Failure 400 {object} ErrorResponse "Invalid request payload, invalid ratios, or invalid shop ID"
// @Failure 409 {object} ErrorResponse "A synchronization job is already running for this tenant"
// @Failure 500 {object} ErrorResponse "Internal database or server error"
// @Router /api/jobs [post]
func (h *Handler) handleStartJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

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
		if req.BaseTime != nil && strings.TrimSpace(*req.BaseTime) != "" {
			if parsed, err := parseFlexTime(strings.TrimSpace(*req.BaseTime), false); err == nil {
				cfg.BaseTime = parsed
			}
			if tsProvider, ok := tenant.GetStorage().(interface {
				GetMaxSupplierTimestamp(context.Context) (time.Time, error)
			}); ok {
				if maxTs, err := tsProvider.GetMaxSupplierTimestamp(ctx); err == nil && !maxTs.IsZero() {
					if cfg.BaseTime.IsZero() || !cfg.BaseTime.After(maxTs) {
						cfg.BaseTime = maxTs.Add(24 * time.Hour)
					}
				}
			}
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
	tenant.Lock()

	// Check 2a: In-memory active job (fast path)
	if tenant.GetActiveJobID() != "" {
		activeID := tenant.GetActiveJobID()
		tenant.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", activeID), ErrCodeJobConflict)
		return
	}

	// Check 2b: Persistent storage active job
	active, err := tenant.GetStorage().GetActiveJob(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
		tenant.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed to check active jobs: "+err.Error(), ErrCodeInternalError)
		return
	}
	if active != nil {
		// If there is no active in-memory runner and the DB job hasn't updated for > 15s, auto-recover it as INTERRUPTED
		if time.Since(active.UpdatedAt) > 15*time.Second {
			_ = tenant.GetStorage().UpdateJobStatus(ctx, active.JobID, domain.JobStatusInterrupted)
		} else {
			tenant.Unlock()
			h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", active.JobID), ErrCodeJobConflict)
			return
		}
	}

	// 3. Atomically create initial job state in storage while holding lock
	now := time.Now().UTC()
	type persistedJobSpec struct {
		Mode            string            `json:"mode"`
		GeneratorConfig *generator.Config `json:"generator_config,omitempty"`
		Batches         []domain.Batch    `json:"batches,omitempty"`
	}
	spec := persistedJobSpec{Mode: req.Mode}
	if req.Mode == "batches" {
		spec.Batches = directBatches
	} else {
		cfgCopy := cfg
		spec.GeneratorConfig = &cfgCopy
	}
	specBytes, _ := json.Marshal(spec)

	job := &domain.SyncJob{
		JobID:               jobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        totalBatches,
		CommittedBatchIndex: -1,
		Summary:             domain.ChangeSummary{},
		JobConfig:           string(specBytes),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := tenant.GetStorage().CreateJob(ctx, job); err != nil {
		tenant.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed to create sync job: "+err.Error(), ErrCodeInternalError)
		return
	}

	// 4. Mark job active in memory while holding lock
	bgCtx, cancel := context.WithCancel(context.Background())
	tenant.SetActiveJobID(jobID)
	tenant.SetActiveCancel(cancel)
	if req.Mode == "generator" {
		tenant.SetActiveConfig(jobID, cfg)
	}
	tenant.Unlock()

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
	tenant.GetBroker().Publish(Event{
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
			tenant.Lock()
			if tenant.GetActiveJobID() == jobID {
				tenant.ResetActive()
			}
			tenant.Unlock()
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

		finalJob, err := tenant.GetEngine().RunJob(bgCtx, jobID, stream)
		if err != nil {
			_ = tenant.GetStorage().UpdateJobStatus(context.Background(), jobID, domain.JobStatusInterrupted)
			var committedIdx int = -1
			var sum domain.ChangeSummary
			if finalJob != nil {
				committedIdx = finalJob.CommittedBatchIndex
				sum = finalJob.Summary
			}
			tenant.GetBroker().Publish(Event{
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

		tenant.GetBroker().Publish(Event{
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

// @Summary Resume interrupted synchronization job
// @Description Resumes an interrupted catalog synchronization job from its last committed checkpoint.
// @Tags Jobs
// @Accept json
// @Produce json
// @Param id path string true "Job ID to resume"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Param request body JobResumeRequest false "Optional generator parameters to override on resumption"
// @Success 202 {object} SyncJobResponse "Job successfully resumed and executing in background"
// @Failure 400 {object} ErrorResponse "Missing job ID, malformed body, or invalid config"
// @Failure 404 {object} ErrorResponse "Sync job not found"
// @Failure 409 {object} ErrorResponse "Job already running or already completed"
// @Failure 500 {object} ErrorResponse "Internal server error"
// @Router /api/jobs/{id}/resume [post]
func (h *Handler) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := r.PathValue("id")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "job ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
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
	tenant.Lock()

	// Check 2a: In-memory active job (fast path)
	if tenant.GetActiveJobID() != "" {
		activeID := tenant.GetActiveJobID()
		tenant.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", activeID), ErrCodeJobConflict)
		return
	}

	// Check 2b: Persistent storage active job
	active, err := tenant.GetStorage().GetActiveJob(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
		tenant.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed checking active jobs: "+err.Error(), ErrCodeInternalError)
		return
	}
	if active != nil {
		if time.Since(active.UpdatedAt) > 15*time.Second {
			_ = tenant.GetStorage().UpdateJobStatus(ctx, active.JobID, domain.JobStatusInterrupted)
		} else {
			tenant.Unlock()
			h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", active.JobID), ErrCodeJobConflict)
			return
		}
	}

	// Check 2c: Fetch target job
	job, err := tenant.GetStorage().GetJob(ctx, jobID)
	if err != nil {
		tenant.Unlock()
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "sync job not found: "+jobID, ErrCodeJobNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to lookup job: "+err.Error(), ErrCodeInternalError)
		return
	}

	// Check 2d: Verify target job state
	if job.Status == domain.JobStatusCompleted {
		tenant.Unlock()
		h.writeError(w, http.StatusConflict, "job is already completed and cannot be resumed", ErrCodeJobConflict)
		return
	}
	if job.Status == domain.JobStatusRunning {
		tenant.Unlock()
		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", jobID), ErrCodeJobConflict)
		return
	}

	// 3. Resolve and validate generator/batches config for resumption (including across process restarts)
	var resumeBatches []domain.Batch
	resumeMode := "generator"
	cfg, ok := tenant.GetActiveConfig(jobID)
	if !ok {
		cfg = generator.DefaultConfig()
		cfg.TotalBatches = job.TotalBatches
		if job.JobConfig != "" {
			var persisted struct {
				Mode            string            `json:"mode"`
				GeneratorConfig *generator.Config `json:"generator_config,omitempty"`
				Batches         []domain.Batch    `json:"batches,omitempty"`
			}
			if err := json.Unmarshal([]byte(job.JobConfig), &persisted); err == nil {
				if persisted.Mode == "batches" && len(persisted.Batches) > 0 {
					resumeMode = "batches"
					resumeBatches = persisted.Batches
				} else if persisted.GeneratorConfig != nil {
					cfg = *persisted.GeneratorConfig
					if cfg.ReservoirCapacity <= 0 {
						cfg.ReservoirCapacity = 1000
					}
				}
			}
		}
	}
	if resumeMode == "generator" {
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
			tenant.Unlock()
			h.writeError(w, http.StatusBadRequest, "invalid resume config: "+err.Error(), ErrCodeInvalidRequest)
			return
		}
	}

	resumedTotalBatches := cfg.TotalBatches
	if resumeMode == "batches" {
		resumedTotalBatches = len(resumeBatches)
	}

	// 4. CRITICAL: Atomically transition job status to RUNNING in storage while holding lock
	if err := tenant.GetStorage().UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
		tenant.Unlock()
		h.writeError(w, http.StatusInternalServerError, "failed to update job status: "+err.Error(), ErrCodeInternalError)
		return
	}

	// 5. Mark job active in memory while holding lock
	bgCtx, cancel := context.WithCancel(context.Background())
	tenant.SetActiveJobID(jobID)
	tenant.SetActiveCancel(cancel)
	if resumeMode == "generator" {
		tenant.SetActiveConfig(jobID, cfg)
	}
	tenant.Unlock()

	// 6. Respond 202 Accepted
	now := time.Now().UTC()
	h.writeJSON(w, http.StatusAccepted, SyncJobResponse{
		JobID:               job.JobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        resumedTotalBatches,
		CommittedBatchIndex: job.CommittedBatchIndex,
		Summary:             job.Summary,
		CreatedAt:           job.CreatedAt,
		UpdatedAt:           now,
	})

	// 7. Broadcast job_started event for resumption
	tenant.GetBroker().Publish(Event{
		Type:  "job_started",
		JobID: jobID,
		Data: map[string]any{
			"job_id":             jobID,
			"status":             string(domain.JobStatusRunning),
			"resumed_from_batch": job.CommittedBatchIndex + 1,
			"total_batches":      resumedTotalBatches,
			"timestamp":          now,
		},
	})

	// 8. Spawn resume in background

	go func() {
		defer cancel()
		defer func() {
			tenant.Lock()
			if tenant.GetActiveJobID() == jobID {
				tenant.ResetActive()
			}
			tenant.Unlock()
		}()

		var stream <-chan domain.Batch
		if resumeMode == "batches" {
			ch := make(chan domain.Batch, len(resumeBatches))
			for _, b := range resumeBatches {
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

		finalJob, err := tenant.GetEngine().ResumeJob(bgCtx, jobID, stream)
		if err != nil {
			_ = tenant.GetStorage().UpdateJobStatus(context.Background(), jobID, domain.JobStatusInterrupted)
			var committedIdx int = -1
			var sum domain.ChangeSummary
			if finalJob != nil {
				committedIdx = finalJob.CommittedBatchIndex
				sum = finalJob.Summary
			}
			tenant.GetBroker().Publish(Event{
				Type:  "job_interrupted",
				JobID: jobID,
				Data: map[string]any{
					"job_id":                jobID,
					"status":                string(domain.JobStatusInterrupted),
					"committed_batch_index": committedIdx,
					"total_batches":         resumedTotalBatches,
					"summary":               sum,
					"reason":                err.Error(),
				},
			})
			return
		}

		tenant.GetBroker().Publish(Event{
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

// @Summary List synchronization jobs
// @Description Returns a paginated list of catalog synchronization jobs, with optional status filtering.
// @Tags Jobs
// @Produce json
// @Param limit query int false "Maximum number of records to return (default 20, min 1)"
// @Param offset query int false "Pagination offset (default 0, min 0)"
// @Param status query string false "Filter by job status (PENDING, RUNNING, INTERRUPTED, COMPLETED, FAILED)"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} JobListResponse "Paginated list of synchronization jobs"
// @Failure 400 {object} ErrorResponse "Invalid limit, offset, or status filter"
// @Failure 500 {object} ErrorResponse "Internal database query error"
// @Router /api/jobs [get]
func (h *Handler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

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

	jobs, total, err := tenant.GetStorage().ListJobsByStatus(ctx, statusFilter, limit, offset)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to query sync jobs: "+err.Error(), ErrCodeInternalError)
		return
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

// @Summary Get synchronization job details
// @Description Retrieves full status and checkpoint audit history for a specific synchronization job.
// @Tags Jobs
// @Produce json
// @Param id path string true "Job ID"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} JobDetailResponse "Job metadata and committed batch checkpoints"
// @Failure 400 {object} ErrorResponse "Missing job ID or invalid request"
// @Failure 404 {object} ErrorResponse "Sync job not found"
// @Failure 500 {object} ErrorResponse "Internal database query error"
// @Router /api/jobs/{id} [get]
func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := r.PathValue("id")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "job ID is required", ErrCodeInvalidRequest)
		return
	}

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	job, err := tenant.GetStorage().GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "sync job not found: "+jobID, ErrCodeJobNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve job: "+err.Error(), ErrCodeInternalError)
		return
	}

	checkpoints, err := tenant.GetStorage().GetBatchCheckpoints(ctx, jobID)
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

// @Summary List quarantined dead-letter records
// @Description Returns paginated dead-letter records with filtering by job ID and quarantine reason.
// @Tags Dead Letter
// @Produce json
// @Param limit query int false "Maximum records to return (default 20, min 1)"
// @Param offset query int false "Pagination offset (default 0, min 0)"
// @Param job_id query string false "Filter records by Job ID"
// @Param reason query string false "Filter records by failure reason"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} DeadLetterListResponse "Paginated list of dead-letter records"
// @Failure 400 {object} ErrorResponse "Invalid limit, offset, or shop ID"
// @Failure 500 {object} ErrorResponse "Internal database query error"
// @Router /api/dead-letter [get]
func (h *Handler) handleListDeadLetter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

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

	issues, total, err := tenant.GetStorage().ListDeadLetterRecords(ctx, jobID, reason, limit, offset)
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

// @Summary Resolve quarantined dead-letter record
// @Description Remediates an invalid dead-letter record and imports the corrected product into the catalog.
// @Tags Dead Letter
// @Accept json
// @Produce json
// @Param id path string true "Dead-letter Issue ID"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Param request body ResolveDeadLetterRequest true "Corrected product details"
// @Success 200 {object} ResolveDeadLetterResponse "Product resolved and imported to catalog"
// @Failure 400 {object} ErrorResponse "Validation error (invalid price, status, or timestamp)"
// @Failure 404 {object} ErrorResponse "Dead-letter issue not found"
// @Failure 500 {object} ErrorResponse "Internal database error"
// @Router /api/dead-letter/{id}/resolve [post]
func (h *Handler) handleResolveDeadLetter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	issueID := r.PathValue("id")
	if strings.TrimSpace(issueID) == "" {
		h.writeError(w, http.StatusBadRequest, "issue ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
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

	if err := tenant.GetStorage().ResolveDeadLetter(ctx, issueID, prod); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "dead letter issue not found", ErrCodeNotFound)
			return
		}
		if errors.Is(err, domain.ErrResurrectionRestricted) || errors.Is(err, domain.ErrOutdatedTimestamp) {
			h.writeError(w, http.StatusConflict, err.Error(), ErrCodeCommitBlocked)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to resolve dead letter record: "+err.Error(), ErrCodeInternalError)
		return
	}

	h.writeJSON(w, http.StatusOK, ResolveDeadLetterResponse{
		Status:  "success",
		Message: "product resolved and imported to catalog",
		Product: *prod,
	})
}

// @Summary List catalog products
// @Description Retrieves a paginated list of catalog products, with optional status filtering and text search.
// @Tags Products
// @Produce json
// @Param limit query int false "Maximum records to return (default 20, min 1)"
// @Param offset query int false "Pagination offset (default 0, min 0)"
// @Param status query string false "Filter by product status (ACTIVE or WITHDRAWN)"
// @Param search query string false "Search substring in SKU or product name"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} ProductListResponse "Paginated catalog products"
// @Failure 400 {object} ErrorResponse "Invalid limit, offset, status, or shop ID"
// @Failure 500 {object} ErrorResponse "Internal database query error"
// @Router /api/products [get]
func (h *Handler) handleListProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

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

	products, total, err := tenant.GetStorage().ListProducts(ctx, status, search, limit, offset)
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

// @Summary Get synchronization metrics and analytics
// @Description Computes aggregated synchronization performance metrics, quality score, and matching sync runs.
// @Tags Metrics
// @Produce json
// @Param time_range query string false "Time preset (today, yesterday, 7d, last7days, all, custom) default 'today'"
// @Param job_id query string false "Filter metrics by specific Job ID"
// @Param from query string false "Custom start timestamp (RFC3339 or YYYY-MM-DD)"
// @Param to query string false "Custom end timestamp (RFC3339 or YYYY-MM-DD)"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} MetricsResponse "Aggregated metrics and matching sync jobs"
// @Failure 400 {object} ErrorResponse "Invalid time_range, date format, or shop ID"
// @Failure 500 {object} ErrorResponse "Internal database error"
// @Router /api/metrics [get]
func (h *Handler) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

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

	agg, jobs, err := tenant.GetStorage().GetMetrics(ctx, filter)
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

// @Summary Preview catalog patch updates
// @Description Performs an immutable, dry-run in-memory projection of a sparse catalog patch without database writes, validating invariants and returning projected item deltas, checksum, and quarantined issues.
// @Tags Patches
// @Accept json
// @Produce json
// @Param X-Shop-Id header string false "Isolated shop tenant ID (default: 'default')"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Param request body domain.CatalogPatch true "Catalog patch payload containing sparse item updates"
// @Success 200 {object} domain.PatchPreviewResult "Dry-run simulation diff preview and invariant checks"
// @Failure 400 {object} ErrorResponse "Malformed JSON, empty items, or invalid shop ID"
// @Failure 500 {object} ErrorResponse "Failed to snapshot catalog or internal server error"
// @Router /api/patches/preview [post]
func (h *Handler) handlePreviewPatch(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	if r.Body == nil {
		h.writeError(w, http.StatusBadRequest, "request body cannot be empty", ErrCodeInvalidRequest)
		return
	}

	var patch domain.CatalogPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
		return
	}

	if len(patch.Items) == 0 {
		h.writeError(w, http.StatusBadRequest, "patch items cannot be empty", ErrCodeInvalidRequest)
		return
	}

	preview, err := catalogSync.PreviewPatch(r.Context(), tenant.GetStorage(), patch)
	if err != nil {
		if errors.Is(err, domain.ErrEmptyPatchItems) {
			h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to preview patch: "+err.Error(), ErrCodeInternalError)
		return
	}

	h.writeJSON(w, http.StatusOK, preview)
}

// @Summary Publish catalog patch updates
// @Description Commits a validated catalog patch atomically to the live catalog, journals pre-image deltas, creates a new PATCH milestone version, and broadcasts real-time SSE notification. Blocked if dead-letter issues are detected.
// @Tags Patches
// @Accept json
// @Produce json
// @Param X-Shop-Id header string false "Isolated shop tenant ID (default: 'default')"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Param request body domain.CatalogPatch true "Catalog patch payload containing sparse item updates"
// @Success 201 {object} PatchPublishResponse "Version published successfully with recorded item deltas"
// @Failure 400 {object} ErrorResponse "Malformed JSON, empty author/description/items, or invalid shop ID"
// @Failure 422 {object} ErrorResponse "Patch commit blocked due to quarantined dead letters or invariant violations"
// @Failure 500 {object} ErrorResponse "Transaction or database storage error"
// @Router /api/patches/publish [post]
func (h *Handler) handlePublishPatch(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	if r.Body == nil {
		h.writeError(w, http.StatusBadRequest, "request body cannot be empty", ErrCodeInvalidRequest)
		return
	}

	var patch domain.CatalogPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
		return
	}

	if err := patch.Validate(); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	engine := catalogSync.NewPatchEngine(tenant.GetStorage())
	ver, deltas, err := engine.PublishPatch(r.Context(), patch)
	if err != nil {
		if errors.Is(err, domain.ErrPatchContainsInvalidRecords) {
			h.writeError(w, http.StatusUnprocessableEntity, err.Error(), ErrCodeCommitBlocked)
			return
		}
		if errors.Is(err, domain.ErrEmptyPatchDescription) ||
			errors.Is(err, domain.ErrEmptyPatchAuthor) ||
			errors.Is(err, domain.ErrEmptyPatchItems) {
			h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to publish patch: "+err.Error(), ErrCodeInternalError)
		return
	}

	// Broadcast SSE event
	tenant.GetBroker().PublishVersionCreated(ver)

	h.writeJSON(w, http.StatusCreated, PatchPublishResponse{
		Version: *ver,
		Deltas:  deltas,
	})
}

// @Summary List catalog versions
// @Description Retrieves a paginated list of catalog version milestones with summary counts and author metadata, ordered newest first.
// @Tags Versions
// @Produce json
// @Param limit query int false "Maximum versions to return (default 20, min 1, max 100)"
// @Param offset query int false "Pagination offset (default 0, min 0)"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {object} VersionListResponse "Paginated list of catalog versions"
// @Failure 400 {object} ErrorResponse "Invalid limit, offset, or shop ID"
// @Failure 500 {object} ErrorResponse "Internal database query error"
// @Router /api/versions [get]
func (h *Handler) handleListVersions(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

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

	versions, total, err := tenant.GetStorage().ListVersions(r.Context(), limit, offset)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to query versions: "+err.Error(), ErrCodeInternalError)
		return
	}
	if versions == nil {
		versions = []domain.CatalogVersion{}
	}

	h.writeJSON(w, http.StatusOK, VersionListResponse{
		Versions: versions,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// @Summary Get catalog version diff
// @Description Retrieves all item-level pre-image deltas (previous and new prices, names, and statuses) associated with a specific catalog version milestone.
// @Tags Versions
// @Produce json
// @Param id path string true "Catalog Version ID"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Success 200 {array} domain.ItemDelta "Array of pre-image item deltas recorded for the version"
// @Failure 400 {object} ErrorResponse "Missing version ID in URL path or invalid shop ID"
// @Failure 404 {object} ErrorResponse "Catalog version not found"
// @Failure 500 {object} ErrorResponse "Internal database query error"
// @Router /api/versions/{id}/diff [get]
func (h *Handler) handleGetVersionDiff(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	versionID := strings.TrimSpace(r.PathValue("id"))
	if versionID == "" {
		h.writeError(w, http.StatusBadRequest, "version ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	// Verify version exists
	_, err = tenant.GetStorage().GetVersion(r.Context(), versionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrVersionNotFound) {
			h.writeError(w, http.StatusNotFound, "catalog version not found: "+versionID, ErrCodeNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve catalog version: "+err.Error(), ErrCodeInternalError)
		return
	}

	deltas, err := tenant.GetStorage().GetVersionDeltas(r.Context(), versionID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve version deltas: "+err.Error(), ErrCodeInternalError)
		return
	}
	if deltas == nil {
		deltas = []domain.ItemDelta{}
	}

	h.writeJSON(w, http.StatusOK, deltas)
}

// @Summary Rollback catalog to target version (Git-revert)
// @Description Reconstructs baseline catalog state at target version, calculates inverse deltas preserving tombstones (Invariant A), updates live catalog, generates a forward ROLLBACK version milestone, and broadcasts real-time SSE notification.
// @Tags Versions
// @Accept json
// @Produce json
// @Param id path string true "Target Catalog Version ID to revert to"
// @Param X-Shop-Id header string false "Isolated shop tenant ID"
// @Param shop query string false "Tenant shop identifier alias"
// @Param shop_id query string false "Tenant shop identifier alias"
// @Param request body RollbackRequest false "Rollback author and audit description"
// @Success 200 {object} RollbackResponse "Catalog reverted successfully with new forward version and deltas"
// @Failure 400 {object} ErrorResponse "Missing version ID in URL path, malformed request body, or invalid shop ID"
// @Failure 404 {object} ErrorResponse "Target catalog version not found"
// @Failure 500 {object} ErrorResponse "Rollback transaction or storage error"
// @Router /api/versions/{id}/rollback [post]
func (h *Handler) handleRollbackVersion(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), ErrCodeInvalidRequest)
		return
	}

	targetVersionID := strings.TrimSpace(r.PathValue("id"))
	if targetVersionID == "" {
		h.writeError(w, http.StatusBadRequest, "version ID is required in URL path", ErrCodeInvalidRequest)
		return
	}

	var req RollbackRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
			return
		}
	}

	author := strings.TrimSpace(req.Author)
	if author == "" {
		author = strings.TrimSpace(req.Operator)
	}
	if author == "" {
		author = "system"
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = fmt.Sprintf("Rollback to version %s", targetVersionID)
	}

	rollbackVer, deltas, err := tenant.GetStorage().RollbackVersion(r.Context(), targetVersionID, author, description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrVersionNotFound) {
			h.writeError(w, http.StatusNotFound, "catalog version not found: "+targetVersionID, ErrCodeNotFound)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to execute rollback: "+err.Error(), ErrCodeInternalError)
		return
	}

	// Broadcast SSE event
	tenant.GetBroker().PublishVersionRollback(rollbackVer)

	h.writeJSON(w, http.StatusOK, RollbackResponse{
		Version: *rollbackVer,
		Deltas:  deltas,
	})
}
