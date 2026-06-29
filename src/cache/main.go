package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-redis/redis/v9"
)

const (
	configPath         = "config.json"
	readOpTimeout      = 4 * time.Second
	writeOpTimeout     = 10 * time.Second
	healthCheckTimeout = 2 * time.Second
)

var errCacheMiss = errors.New("cache miss")

type cacheSetBody struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	TTL   string `json:"ttl"`
}

type Config struct {
	RedisHost string `json:"redis_host"`
	RedisPort int    `json:"redis_port"`
	TTL       int    `json:"ttl"`
}

type CacheItem struct {
	Value string
	TTL   time.Duration
}

type CacheStore interface {
	Ping(context.Context) error
	Get(context.Context, string) (CacheItem, error)
	Set(context.Context, string, string, time.Duration) error
	Close() error
}

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(config *Config) *RedisStore {
	return &RedisStore{
		client: redis.NewClient(&redis.Options{
			Addr:         fmt.Sprintf("%s:%d", config.RedisHost, config.RedisPort),
			Password:     "",
			DB:           0,
			PoolSize:     20,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			DialTimeout:  5 * time.Second,
		}),
	}
}

func (rs *RedisStore) Ping(ctx context.Context) error {
	return rs.client.Ping(ctx).Err()
}

func (rs *RedisStore) Get(ctx context.Context, key string) (CacheItem, error) {
	pipe := rs.client.Pipeline()
	getCmd := pipe.Get(ctx, key)
	ttlCmd := pipe.TTL(ctx, key)

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return CacheItem{}, err
	}

	value, err := getCmd.Result()
	if errors.Is(err, redis.Nil) {
		return CacheItem{}, errCacheMiss
	}
	if err != nil {
		return CacheItem{}, err
	}

	ttl, err := ttlCmd.Result()
	if err != nil {
		return CacheItem{}, err
	}
	if ttl <= 0 {
		return CacheItem{}, errCacheMiss
	}

	return CacheItem{Value: value, TTL: ttl}, nil
}

func (rs *RedisStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("ttl must be greater than zero")
	}
	return rs.client.Set(ctx, key, value, ttl).Err()
}

func (rs *RedisStore) Close() error {
	return rs.client.Close()
}

type CacheService struct {
	config *Config
	store  CacheStore
}

func NewCacheService(config *Config) *CacheService {
	return &CacheService{
		config: config,
		store:  NewRedisStore(config),
	}
}

func newCacheService(config *Config, store CacheStore) *CacheService {
	return &CacheService{
		config: config,
		store:  store,
	}
}

func loadConfig() (*Config, error) {
	return loadConfigFile(configPath)
}

func loadConfigFile(path string) (*Config, error) {
	configFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer configFile.Close()

	byteValue, err := io.ReadAll(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(byteValue, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func (cs *CacheService) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	return cs.store.Ping(ctx)
}

func (cs *CacheService) GetCache(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeCacheError(w, http.StatusBadRequest, map[string]string{"error": "key query parameter is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readOpTimeout)
	defer cancel()

	item, err := cs.store.Get(ctx, key)
	if errors.Is(err, errCacheMiss) {
		writeCacheError(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}
	if err != nil {
		log.Printf("Redis error: %v", err)
		writeCacheError(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if item.TTL <= 0 {
		writeCacheError(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	ttlSeconds := int(item.TTL.Seconds())
	w.Header().Set("X-CACHE-TTL", strconv.Itoa(ttlSeconds))
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", ttlSeconds))
	writeJSON(w, http.StatusOK, item.Value)
}

func (cs *CacheService) SetCache(w http.ResponseWriter, r *http.Request) {
	var requestBody cacheSetBody
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		writeCacheError(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "details": err.Error()})
		return
	}
	if requestBody.Key == "" || requestBody.Value == "" {
		writeCacheError(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "details": "key and value are required"})
		return
	}

	ttl, err := cs.cacheTTL(requestBody.TTL)
	if err != nil {
		writeCacheError(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), writeOpTimeout)
	defer cancel()

	if err := cs.store.Set(ctx, requestBody.Key, requestBody.Value, ttl); err != nil {
		log.Printf("Redis set error: %v", err)
		writeCacheError(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "cached"})
}

func (cs *CacheService) cacheTTL(rawTTL string) (time.Duration, error) {
	if rawTTL == "" {
		if cs.config.TTL <= 0 {
			return 0, fmt.Errorf("ttl must be greater than zero")
		}
		return time.Duration(cs.config.TTL) * time.Second, nil
	}

	ttlInt, err := strconv.Atoi(rawTTL)
	if err != nil {
		return 0, fmt.Errorf("ttl must be a string representation of an integer")
	}
	if ttlInt <= 0 {
		return 0, fmt.Errorf("ttl must be greater than zero")
	}

	return time.Duration(ttlInt) * time.Second, nil
}

func (cs *CacheService) Close() error {
	return cs.store.Close()
}

func newRouter(cacheService *CacheService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", cacheService.healthHandler)
	mux.HandleFunc("/api/health", cacheService.healthHandler)
	mux.HandleFunc("/api/cache", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			cacheService.GetCache(w, r)
		case http.MethodPost:
			cacheService.SetCache(w, r)
		default:
			writeNoStore(w)
			http.NotFound(w, r)
		}
	})
	return mux
}

func (cs *CacheService) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeNoStore(w)
		http.NotFound(w, r)
		return
	}
	if err := cs.HealthCheck(r.Context()); err != nil {
		writeNoStore(w)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Redis connection failed"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func writeCacheError(w http.ResponseWriter, status int, payload map[string]string) {
	writeNoStore(w)
	writeJSON(w, status, payload)
}

func writeNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		writeNoStore(w)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func runHealthcheck() int {
	url := os.Getenv("CACHE_HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8080/health"
	}

	return runHealthcheckWithClient(&http.Client{Timeout: healthCheckTimeout}, url)
}

func runHealthcheckWithClient(client *http.Client, url string) int {
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("healthcheck request failed: %v", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("healthcheck returned status %d", resp.StatusCode)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cacheService := NewCacheService(config)
	defer cacheService.Close()

	if err := cacheService.HealthCheck(context.Background()); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      newRouter(cacheService),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println("Starting server on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exiting")
}
