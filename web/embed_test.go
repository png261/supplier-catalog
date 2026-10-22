package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		if !strings.Contains(body, "Supplier Catalog") {
			t.Errorf("expected dashboard title in index.html, got: %s", body)
		}
		cc := rec.Header().Get("Cache-Control")
		if !strings.Contains(cc, "no-cache") {
			t.Errorf("expected no-cache Cache-Control, got: %s", cc)
		}
	})

	t.Run("Serve /index.html directly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Supplier Catalog") {
			t.Errorf("expected dashboard content")
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
		if !strings.Contains(rec.Body.String(), "Supplier Catalog") {
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

	t.Run("Path without leading slash", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
		req.URL.Path = "style.css" // simulate path without leading slash
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("Static prefix root /static", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/static", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})
}

func TestAssetHandler_DiskDir(t *testing.T) {
	tempDir := t.TempDir()
	indexFile := filepath.Join(tempDir, "index.html")
	if err := os.WriteFile(indexFile, []byte("<html><body>Custom Disk Asset</body></html>"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	handler := AssetHandler(tempDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Custom Disk Asset") {
		t.Errorf("expected custom disk asset body, got: %s", rec.Body.String())
	}
}

func TestFileExists_EdgeCases(t *testing.T) {
	if fileExists(nil, "something") {
		t.Error("expected false for nil FS")
	}
	if fileExists(staticFS, "") {
		t.Error("expected false for empty name")
	}
	if fileExists(staticFS, "static") {
		// "static" is a directory, not a file
		t.Error("expected false for directory")
	}
	if fileExists(staticFS, "nonexistent-file.xyz") {
		t.Error("expected false for nonexistent file")
	}
}
