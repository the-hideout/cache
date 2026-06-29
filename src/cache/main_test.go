package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu      sync.Mutex
	pingErr error
	getErr  error
	setErr  error
	items   map[string]CacheItem
	sets    []setCall
}

type setCall struct {
	key   string
	value string
	ttl   time.Duration
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: make(map[string]CacheItem)}
}

func (f *fakeStore) Ping(context.Context) error {
	return f.pingErr
}

func (f *fakeStore) Get(_ context.Context, key string) (CacheItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.getErr != nil {
		return CacheItem{}, f.getErr
	}
	item, ok := f.items[key]
	if !ok {
		return CacheItem{}, errCacheMiss
	}
	return item, nil
}

func (f *fakeStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.setErr != nil {
		return f.setErr
	}
	f.items[key] = CacheItem{Value: value, TTL: ttl}
	f.sets = append(f.sets, setCall{key: key, value: value, ttl: ttl})
	return nil
}

func (f *fakeStore) Close() error {
	return nil
}

func testConfig() *Config {
	return &Config{
		RedisHost: "localhost",
		RedisPort: 6379,
		TTL:       300,
	}
}

func testRouter(store *fakeStore) http.Handler {
	return newRouter(newCacheService(testConfig(), store))
}

func serve(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, status, w.Body.String())
	}
}

func requireBody(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := w.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func requireJSONField(t *testing.T, w *httptest.ResponseRecorder, key, want string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v", err)
	}
	if body[key] != want {
		t.Fatalf("body[%q] = %q, want %q", key, body[key], want)
	}
}

func TestNewCacheService(t *testing.T) {
	service := NewCacheService(testConfig())
	if service == nil {
		t.Fatal("service is nil")
	}
	if service.config == nil {
		t.Fatal("service config is nil")
	}
	if service.store == nil {
		t.Fatal("service store is nil")
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := []byte(`{"redis_host":"redis","redis_port":6379,"ttl":500}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.RedisHost != "redis" {
		t.Fatalf("RedisHost = %q, want redis", config.RedisHost)
	}
	if config.RedisPort != 6379 {
		t.Fatalf("RedisPort = %d, want 6379", config.RedisPort)
	}
	if config.TTL != 500 {
		t.Fatalf("TTL = %d, want 500", config.TTL)
	}
}

func TestLoadConfigDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	data := []byte(`{"redis_host":"redis","redis_port":6379,"ttl":500}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.TTL != 500 {
		t.Fatalf("TTL = %d, want 500", config.TTL)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	if _, err := loadConfigFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing config error is nil")
	}

	dir := t.TempDir()
	if _, err := loadConfigFile(dir); err == nil {
		t.Fatal("directory config error is nil")
	}

	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"redis_host":`), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	if _, err := loadConfigFile(path); err == nil {
		t.Fatal("invalid config error is nil")
	}
}

func TestHealthEndpoints(t *testing.T) {
	router := testRouter(newFakeStore())
	for _, path := range []string{"/health", "/api/health"} {
		t.Run(path, func(t *testing.T) {
			w := serve(router, http.MethodGet, path, "")
			requireStatus(t, w, http.StatusOK)
			requireBody(t, w, "OK")
		})
	}
}

func TestHealthEndpointsWrongMethodsReturn404(t *testing.T) {
	router := testRouter(newFakeStore())
	for _, path := range []string{"/health", "/api/health"} {
		t.Run(path, func(t *testing.T) {
			w := serve(router, http.MethodPost, path, "")
			requireStatus(t, w, http.StatusNotFound)
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestHealthEndpointsWithRedisFailure(t *testing.T) {
	store := newFakeStore()
	store.pingErr = errors.New("redis down")
	router := testRouter(store)

	for _, path := range []string{"/health", "/api/health"} {
		t.Run(path, func(t *testing.T) {
			w := serve(router, http.MethodGet, path, "")
			requireStatus(t, w, http.StatusServiceUnavailable)
			requireBody(t, w, "Redis connection failed")
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestCacheEndpointWrongMethodsReturn404(t *testing.T) {
	router := testRouter(newFakeStore())
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			w := serve(router, method, "/api/cache", "")
			requireStatus(t, w, http.StatusNotFound)
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestGetCache(t *testing.T) {
	store := newFakeStore()
	store.items["existing-key"] = CacheItem{Value: "existing-value", TTL: 300 * time.Second}
	router := testRouter(store)

	tests := []struct {
		name       string
		path       string
		status     int
		body       string
		cacheTTL   string
		cacheCtl   string
		noStore    bool
		jsonErrMsg string
	}{
		{
			name:     "existing key",
			path:     "/api/cache?key=existing-key",
			status:   http.StatusOK,
			body:     `"existing-value"`,
			cacheTTL: "300",
			cacheCtl: "public, max-age=300",
		},
		{
			name:       "missing key",
			path:       "/api/cache?key=missing",
			status:     http.StatusNotFound,
			noStore:    true,
			cacheCtl:   "no-store",
			jsonErrMsg: "key not found",
		},
		{
			name:       "missing key parameter",
			path:       "/api/cache",
			status:     http.StatusBadRequest,
			noStore:    true,
			cacheCtl:   "no-store",
			jsonErrMsg: "key query parameter is required",
		},
		{
			name:       "empty key parameter",
			path:       "/api/cache?key=",
			status:     http.StatusBadRequest,
			noStore:    true,
			cacheCtl:   "no-store",
			jsonErrMsg: "key query parameter is required",
		},
		{
			name:       "first key parameter is used",
			path:       "/api/cache?key=missing&key=existing-key",
			status:     http.StatusNotFound,
			noStore:    true,
			cacheCtl:   "no-store",
			jsonErrMsg: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(router, http.MethodGet, tt.path, "")
			requireStatus(t, w, tt.status)
			if tt.body != "" {
				requireBody(t, w, tt.body)
			}
			if tt.jsonErrMsg != "" {
				requireJSONField(t, w, "error", tt.jsonErrMsg)
			}
			if got := w.Header().Get("X-CACHE-TTL"); got != tt.cacheTTL {
				t.Fatalf("X-CACHE-TTL = %q, want %q", got, tt.cacheTTL)
			}
			if got := w.Header().Get("Cache-Control"); got != tt.cacheCtl {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.cacheCtl)
			}
			if tt.noStore && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestHeadCacheUsesGetHeaders(t *testing.T) {
	store := newFakeStore()
	store.items["existing-key"] = CacheItem{Value: "existing-value", TTL: 300 * time.Second}

	w := serve(testRouter(store), http.MethodHead, "/api/cache?key=existing-key", "")
	requireStatus(t, w, http.StatusOK)
	if got := w.Header().Get("X-CACHE-TTL"); got != "300" {
		t.Fatalf("X-CACHE-TTL = %q, want 300", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("Cache-Control = %q, want public, max-age=300", got)
	}
}

func TestGetCacheErrors(t *testing.T) {
	t.Run("store error", func(t *testing.T) {
		store := newFakeStore()
		store.getErr = errors.New("redis failed")
		w := serve(testRouter(store), http.MethodGet, "/api/cache?key=boom", "")
		requireStatus(t, w, http.StatusInternalServerError)
		requireJSONField(t, w, "error", "internal server error")
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("non positive ttl is treated as miss", func(t *testing.T) {
		store := newFakeStore()
		store.items["stale"] = CacheItem{Value: "stale", TTL: 0}
		w := serve(testRouter(store), http.MethodGet, "/api/cache?key=stale", "")
		requireStatus(t, w, http.StatusNotFound)
		requireJSONField(t, w, "error", "key not found")
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", got)
		}
	})
}

func TestSetCache(t *testing.T) {
	store := newFakeStore()
	router := testRouter(store)

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		expectedTTL    time.Duration
		expectedKey    string
		expectedValue  string
		expectedError  string
	}{
		{
			name:           "default ttl",
			body:           `{"key":"default","value":"default value"}`,
			expectedStatus: http.StatusOK,
			expectedTTL:    300 * time.Second,
			expectedKey:    "default",
			expectedValue:  "default value",
		},
		{
			name:           "custom ttl",
			body:           `{"key":"custom","value":"custom value","ttl":"60"}`,
			expectedStatus: http.StatusOK,
			expectedTTL:    60 * time.Second,
			expectedKey:    "custom",
			expectedValue:  "custom value",
		},
		{
			name:           "leading zero ttl",
			body:           `{"key":"leading","value":"leading value","ttl":"0030"}`,
			expectedStatus: http.StatusOK,
			expectedTTL:    30 * time.Second,
			expectedKey:    "leading",
			expectedValue:  "leading value",
		},
		{
			name:           "missing key",
			body:           `{"value":"test"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:           "missing value",
			body:           `{"key":"test"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:           "empty key",
			body:           `{"key":"","value":"test"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:           "empty value",
			body:           `{"key":"test","value":""}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:           "invalid json",
			body:           `invalid json`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:           "invalid ttl",
			body:           `{"key":"test","value":"test","ttl":"not_a_number"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "ttl must be a string representation of an integer",
		},
		{
			name:           "negative ttl",
			body:           `{"key":"test","value":"test","ttl":"-1"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "ttl must be greater than zero",
		},
		{
			name:           "zero ttl",
			body:           `{"key":"test","value":"test","ttl":"0"}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "ttl must be greater than zero",
		},
		{
			name:           "numeric ttl",
			body:           `{"key":"test","value":"test","ttl":30}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(router, http.MethodPost, "/api/cache", tt.body)
			requireStatus(t, w, tt.expectedStatus)
			if tt.expectedStatus != http.StatusOK {
				requireJSONField(t, w, "error", tt.expectedError)
				if got := w.Header().Get("Cache-Control"); got != "no-store" {
					t.Fatalf("Cache-Control = %q, want no-store", got)
				}
				return
			}

			requireBody(t, w, `{"message":"cached"}`)
			store.mu.Lock()
			defer store.mu.Unlock()
			item, ok := store.items[tt.expectedKey]
			if !ok {
				t.Fatalf("key %q was not stored", tt.expectedKey)
			}
			if item.Value != tt.expectedValue {
				t.Fatalf("stored value = %q, want %q", item.Value, tt.expectedValue)
			}
			if item.TTL != tt.expectedTTL {
				t.Fatalf("stored ttl = %s, want %s", item.TTL, tt.expectedTTL)
			}
		})
	}
}

func TestSetCacheStoreFailure(t *testing.T) {
	store := newFakeStore()
	store.setErr = errors.New("redis failed")

	w := serve(testRouter(store), http.MethodPost, "/api/cache", `{"key":"test","value":"test"}`)
	requireStatus(t, w, http.StatusInternalServerError)
	requireJSONField(t, w, "error", "internal server error")
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestRoundTripCacheValue(t *testing.T) {
	store := newFakeStore()
	router := testRouter(store)

	post := serve(router, http.MethodPost, "/api/cache", `{"key":"unicode","value":"Hello 世界","ttl":"120"}`)
	requireStatus(t, post, http.StatusOK)

	get := serve(router, http.MethodGet, "/api/cache?key=unicode", "")
	requireStatus(t, get, http.StatusOK)
	requireBody(t, get, `"Hello 世界"`)
	if got := get.Header().Get("X-CACHE-TTL"); got != "120" {
		t.Fatalf("X-CACHE-TTL = %q, want 120", got)
	}
	if got := get.Header().Get("Cache-Control"); got != "public, max-age=120" {
		t.Fatalf("Cache-Control = %q, want public, max-age=120", got)
	}
}

func TestCacheTTLRejectsInvalidDefault(t *testing.T) {
	service := newCacheService(&Config{TTL: 0}, newFakeStore())
	if _, err := service.cacheTTL(""); err == nil {
		t.Fatal("expected error for invalid default ttl")
	}
}

func TestRedisStoreRejectsInvalidSetTTL(t *testing.T) {
	store := &RedisStore{}
	if err := store.Set(context.Background(), "key", "value", 0); err == nil {
		t.Fatal("expected error for invalid ttl")
	}
}

func TestWriteJSONMarshalError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]chan int{"bad": make(chan int)})
	requireStatus(t, w, http.StatusInternalServerError)
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestRunHealthcheck(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("OK")),
			}, nil
		})}
		if code := runHealthcheckWithClient(client, "http://cache.local/health"); code != 0 {
			t.Fatalf("healthcheck exit = %d, want 0", code)
		}
	})

	t.Run("failure", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader("Redis connection failed")),
			}, nil
		})}
		if code := runHealthcheckWithClient(client, "http://cache.local/health"); code != 1 {
			t.Fatalf("healthcheck exit = %d, want 1", code)
		}
	})

	t.Run("request error", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		})}
		if code := runHealthcheckWithClient(client, "http://cache.local/health"); code != 1 {
			t.Fatalf("healthcheck exit = %d, want 1", code)
		}
	})

	t.Run("invalid env url", func(t *testing.T) {
		t.Setenv("CACHE_HEALTHCHECK_URL", "://bad-url")
		if code := runHealthcheck(); code != 1 {
			t.Fatalf("healthcheck exit = %d, want 1", code)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func BenchmarkSetCache(b *testing.B) {
	router := testRouter(newFakeStore())
	body := []byte(`{"key":"benchmark-key","value":"benchmark-value","ttl":"300"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/cache", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status = %d", w.Code)
		}
	}
}

func BenchmarkGetCache(b *testing.B) {
	store := newFakeStore()
	store.items["benchmark-key"] = CacheItem{Value: "benchmark-value", TTL: 300 * time.Second}
	router := testRouter(store)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/cache?key=benchmark-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status = %d", w.Code)
		}
	}
}

func FuzzCacheTTL(f *testing.F) {
	service := newCacheService(testConfig(), newFakeStore())
	for _, seed := range []string{"", "1", "0030", "0", "-1", "1e2", "10.5", "9223372036854775808"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		ttl, err := service.cacheTTL(raw)
		if raw == "" {
			if err != nil || ttl != 300*time.Second {
				t.Fatalf("default ttl = %s, err = %v", ttl, err)
			}
			return
		}

		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			if err == nil {
				t.Fatalf("cacheTTL(%q) succeeded for invalid input", raw)
			}
			return
		}
		if err != nil {
			t.Fatalf("cacheTTL(%q) error: %v", raw, err)
		}
		if ttl != time.Duration(parsed)*time.Second {
			t.Fatalf("cacheTTL(%q) = %s, want %s", raw, ttl, time.Duration(parsed)*time.Second)
		}
	})
}
