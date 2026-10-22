package sync_test

import (
	"testing"

	"coderpush/pkg/storage"
)

func setupTestStorage(t *testing.T) *storage.SQLiteStorage {
	t.Helper()
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to create in-memory storage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
