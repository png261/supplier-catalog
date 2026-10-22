package api

import (
	"net/http"

	_ "coderpush/docs"
	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
	"coderpush/web"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// ServerOption configures server behavior.
type ServerOption func(*serverConfig)

type serverConfig struct {
	dataDir string
}

// WithDataDir configures the persistent base data directory for multi-tenant shop databases.
func WithDataDir(dataDir string) ServerOption {
	return func(cfg *serverConfig) {
		cfg.dataDir = dataDir
	}
}

// NewServer builds and returns the complete HTTP handler stack combining REST API and static frontend assets.
func NewServer(store storage.Storage, eng catalogSync.Engine, broker *Broker, staticDir string, opts ...ServerOption) http.Handler {
	cfg := &serverConfig{dataDir: "."}
	for _, opt := range opts {
		opt(cfg)
	}

	handler := NewHandler(store, eng, broker, WithHandlerDataDir(cfg.dataDir))

	rootMux := http.NewServeMux()
	handler.RegisterRoutes(rootMux)
	rootMux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))
	rootMux.Handle("/", web.AssetHandler(staticDir))

	return Recoverer(Logging(CORS(rootMux)))
}

// NewProgressHook creates a progress callback that publishes real-time progress and dead-letter events to the SSE broker.
func NewProgressHook(broker *Broker) catalogSync.ProgressHook {
	return func(jobID string, batchIndex, totalBatches int, delta, cumulative domain.ChangeSummary) {
		pct := 0.0
		if totalBatches > 0 {
			pct = float64(batchIndex+1) / float64(totalBatches) * 100.0
		}
		broker.Publish(Event{
			Type:  "progress",
			JobID: jobID,
			Data: map[string]any{
				"job_id":             jobID,
				"batch_index":        batchIndex,
				"total_batches":      totalBatches,
				"percent":            pct,
				"batch_delta":        delta,
				"cumulative_summary": cumulative,
			},
		})
		if delta.DeadLetter > 0 {
			broker.Publish(Event{
				Type:  "dead_letter",
				JobID: jobID,
				Data: map[string]any{
					"job_id":            jobID,
					"batch_index":       batchIndex,
					"new_issues":        delta.DeadLetter,
					"total_dead_letter": cumulative.DeadLetter,
				},
			})
		}
	}
}
