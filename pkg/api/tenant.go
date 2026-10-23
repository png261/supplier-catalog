package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"coderpush/pkg/generator"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

var validShopIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// tenantOps abstracts the operations for an isolated shop/tenant context.
type tenantOps interface {
	GetID() string
	GetStorage() storage.Storage
	GetEngine() catalogSync.Engine
	GetBroker() *Broker
	Lock()
	Unlock()
	GetActiveJobID() string
	SetActiveJobID(id string)
	GetActiveCancel() context.CancelFunc
	SetActiveCancel(cancel context.CancelFunc)
	GetActiveConfig(jobID string) (generator.Config, bool)
	SetActiveConfig(jobID string, cfg generator.Config)
	RemoveActiveConfig(jobID string)
	ResetActive()
}

// TenantContext encapsulates the isolated runtime resources for a single shop/tenant.
type TenantContext struct {
	ID                 string
	Storage            storage.Storage
	Engine             catalogSync.Engine
	Broker             *Broker
	Mu                 sync.Mutex
	ActiveJobID        string
	ActiveCancel       context.CancelFunc
	ActiveConfigs      map[string]generator.Config
	lastAccessUnixNano atomic.Int64
}

func (t *TenantContext) GetID() string                             { return t.ID }
func (t *TenantContext) GetStorage() storage.Storage               { return t.Storage }
func (t *TenantContext) GetEngine() catalogSync.Engine             { return t.Engine }
func (t *TenantContext) GetBroker() *Broker                        { return t.Broker }
func (t *TenantContext) Lock()                                     { t.Mu.Lock() }
func (t *TenantContext) Unlock()                                   { t.Mu.Unlock() }
func (t *TenantContext) GetActiveJobID() string                    { return t.ActiveJobID }
func (t *TenantContext) SetActiveJobID(id string)                  { t.ActiveJobID = id }
func (t *TenantContext) GetActiveCancel() context.CancelFunc       { return t.ActiveCancel }
func (t *TenantContext) SetActiveCancel(cancel context.CancelFunc) { t.ActiveCancel = cancel }

func (t *TenantContext) GetActiveConfig(jobID string) (generator.Config, bool) {
	if t.ActiveConfigs == nil {
		return generator.Config{}, false
	}
	cfg, ok := t.ActiveConfigs[jobID]
	return cfg, ok
}

func (t *TenantContext) SetActiveConfig(jobID string, cfg generator.Config) {
	if t.ActiveConfigs == nil {
		t.ActiveConfigs = make(map[string]generator.Config)
	}
	t.ActiveConfigs[jobID] = cfg
}

func (t *TenantContext) RemoveActiveConfig(jobID string) {
	if t.ActiveConfigs != nil {
		delete(t.ActiveConfigs, jobID)
	}
}

func (t *TenantContext) ResetActive() {
	t.ActiveJobID = ""
	t.ActiveCancel = nil
}

// Touch updates the last access timestamp to the current time atomically.
func (t *TenantContext) Touch() {
	t.lastAccessUnixNano.Store(time.Now().UnixNano())
}

// GetLastAccess returns the last access time atomically as time.Time.
func (t *TenantContext) GetLastAccess() time.Time {
	nano := t.lastAccessUnixNano.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// LastAccess returns the last access time atomically as time.Time.
func (t *TenantContext) LastAccess() time.Time {
	return t.GetLastAccess()
}

// SetLastAccess explicitly sets the last access time atomically (primarily for testing and lifecycle management).
func (t *TenantContext) SetLastAccess(at time.Time) {
	if at.IsZero() {
		t.lastAccessUnixNano.Store(0)
		return
	}
	t.lastAccessUnixNano.Store(at.UnixNano())
}

// Ensure TenantContext implements tenantOps.
var _ tenantOps = (*TenantContext)(nil)

// TenantManager coordinates dynamic SQLite storage and brokers per shop/tenant.
type TenantManager struct {
	baseDir       string
	defaultTenant tenantOps
	mu            sync.RWMutex
	tenants       map[string]*TenantContext
	ctx           context.Context
	cancel        context.CancelFunc
	cleanupTicker *time.Ticker
	maxIdleAge    time.Duration
}

// NewTenantManager initializes a manager for multi-tenant shop environments.
func NewTenantManager(baseDir string, defaultTenant tenantOps) *TenantManager {
	if baseDir == "" {
		baseDir = "."
	}
	ctx, cancel := context.WithCancel(context.Background())

	maxIdleAge := 1 * time.Hour
	if envTTL := os.Getenv("SHOP_TTL"); envTTL != "" {
		if d, err := time.ParseDuration(envTTL); err == nil && d > 0 {
			maxIdleAge = d
		}
	}

	tm := &TenantManager{
		baseDir:       baseDir,
		defaultTenant: defaultTenant,
		tenants:       make(map[string]*TenantContext),
		ctx:           ctx,
		cancel:        cancel,
		maxIdleAge:    maxIdleAge,
	}

	// Run background cleaner every 5 minutes to prune shops inactive for >= 1 hour
	tm.cleanupTicker = time.NewTicker(5 * time.Minute)
	go tm.runCleaner()

	return tm
}

// GetOrCreate retrieves an existing tenant or dynamically boots a new shop database.
func (tm *TenantManager) GetOrCreate(shopID string) (*TenantContext, error) {
	shopID = strings.ToLower(strings.TrimSpace(shopID))
	if !validShopIDRegex.MatchString(shopID) {
		return nil, fmt.Errorf("invalid shop ID %q: must be 1-64 alphanumeric characters, dashes, or underscores", shopID)
	}

	tm.mu.RLock()
	if t, exists := tm.tenants[shopID]; exists {
		t.Touch()
		tm.mu.RUnlock()
		return t, nil
	}
	tm.mu.RUnlock()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check after acquiring write lock
	if t, exists := tm.tenants[shopID]; exists {
		t.Touch()
		return t, nil
	}

	shopsDir := filepath.Join(tm.baseDir, "shops")
	if err := os.MkdirAll(shopsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating shops directory: %w", err)
	}

	dbPath := filepath.Join(shopsDir, fmt.Sprintf("%s.db", shopID))
	store, err := storage.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed opening SQLite storage for shop %q: %w", shopID, err)
	}

	// Startup crash recovery for this shop
	if _, err := store.RecoverInterruptedJobs(tm.ctx); err != nil {
		log.Printf("[tenant-manager] Warning: recovery failed for shop %q: %v", shopID, err)
	}

	// Dedicated SSE Broker for this shop
	broker := NewBroker()
	go broker.Start(tm.ctx)

	// Engine hooked to this shop's broker
	hook := NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))

	tenant := &TenantContext{
		ID:            shopID,
		Storage:       store,
		Engine:        eng,
		Broker:        broker,
		ActiveConfigs: make(map[string]generator.Config),
	}
	tenant.Touch()

	tm.tenants[shopID] = tenant
	return tenant, nil
}

// Close gracefully closes all tenant databases and brokers.
func (tm *TenantManager) Close() {
	if tm.cancel != nil {
		tm.cancel()
	}
	if tm.cleanupTicker != nil {
		tm.cleanupTicker.Stop()
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, t := range tm.tenants {
		t.Broker.Close()
		_ = t.Storage.Close()
	}
	tm.tenants = make(map[string]*TenantContext)
}

// runCleaner runs periodic pruning of stale demo shops.
func (tm *TenantManager) runCleaner() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-tm.cleanupTicker.C:
			tm.PruneStaleTenants()
		}
	}
}

// PruneStaleTenants evicts inactive tenants from memory and removes old DB files.
func (tm *TenantManager) PruneStaleTenants() {
	tm.mu.Lock()
	now := time.Now()
	shopsDir := filepath.Join(tm.baseDir, "shops")

	for id, t := range tm.tenants {
		t.Mu.Lock()
		activeJob := t.ActiveJobID
		t.Mu.Unlock()

		if activeJob == "" && now.Sub(t.GetLastAccess()) >= tm.maxIdleAge {
			t.Broker.Close()
			_ = t.Storage.Close()
			delete(tm.tenants, id)

			// Remove files from disk immediately
			_ = os.Remove(filepath.Join(shopsDir, id+".db"))
			_ = os.Remove(filepath.Join(shopsDir, id+".db-wal"))
			_ = os.Remove(filepath.Join(shopsDir, id+".db-shm"))
			log.Printf("[tenant-manager] Auto-removed inactive shop %q after %v", id, tm.maxIdleAge)
		}
	}
	tm.mu.Unlock()

	// Prune orphan disk files older than maxIdleAge
	entries, err := os.ReadDir(shopsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".db") {
			info, err := entry.Info()
			if err == nil && now.Sub(info.ModTime()) >= tm.maxIdleAge {
				base := strings.TrimSuffix(entry.Name(), ".db")
				tm.mu.RLock()
				_, inUse := tm.tenants[base]
				tm.mu.RUnlock()
				if !inUse {
					_ = os.Remove(filepath.Join(shopsDir, entry.Name()))
					_ = os.Remove(filepath.Join(shopsDir, entry.Name()+"-wal"))
					_ = os.Remove(filepath.Join(shopsDir, entry.Name()+"-shm"))
					log.Printf("[tenant-manager] Auto-removed expired demo shop database %s", entry.Name())
				}
			}
		}
	}
}

// ExtractShopID parses shop tenant ID from HTTP Request (Header 'X-Shop-Id' or Query 'shop'/'shop_id').
func ExtractShopID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id := strings.TrimSpace(r.Header.Get("X-Shop-Id")); id != "" {
		return strings.ToLower(id)
	}
	if id := strings.TrimSpace(r.URL.Query().Get("shop")); id != "" {
		return strings.ToLower(id)
	}
	if id := strings.TrimSpace(r.URL.Query().Get("shop_id")); id != "" {
		return strings.ToLower(id)
	}
	return ""
}
