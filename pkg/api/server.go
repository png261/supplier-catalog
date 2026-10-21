package api

import (
	"net/http"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
	"coderpush/web"
)

// NewServer builds and returns the complete HTTP handler stack combining REST API and static frontend assets.
func NewServer(store storage.Storage, eng catalogSync.Engine, broker *Broker, staticDir string) http.Handler {
	handler := NewHandler(store, eng, broker)

	rootMux := http.NewServeMux()
	handler.RegisterRoutes(rootMux)
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
