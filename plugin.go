// @TODO Simplify naming in the package
package traefik_plugin_http_cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-redis/redis/v8"
)

type Config struct {
	RedisAddr string `json:"redisAddr,omitempty"`
	TTL       int    `json:"ttl,omitempty"` // seconds
}

func CreateConfig() *Config {
	return &Config{
		RedisAddr: "redis:6379",
		TTL:       60,
	}
}

type CacheMiddleware struct {
	next  http.Handler
	redis *redis.Client
	ttl   time.Duration
	name  string
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	client := redis.NewClient(&redis.Options{
		Addr: config.RedisAddr,
	})
	return &CacheMiddleware{
		next:  next,
		redis: client,
		ttl:   time.Duration(config.TTL) * time.Second,
		name:  name,
	}, nil
}

func (m *CacheMiddleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		m.next.ServeHTTP(rw, req)
		return
	}

	fmt.Println("Request URI:", req.RequestURI)

	cacheKey := m.hashRequest(req)

	ctx := req.Context()
	cached, err := m.redis.Get(ctx, cacheKey).Result()
	if err == nil {
		rw.Header().Set("X-Traefik-Cache", "HIT")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(cached))
		return
	}

	rec := &responseRecorder{ResponseWriter: rw, status: 200}
	m.next.ServeHTTP(rec, req)

	_ = m.redis.Set(ctx, cacheKey, rec.body.String(), m.ttl).Err()
}

func (m *CacheMiddleware) hashRequest(req *http.Request) string {
	h := sha256.New()
	io.WriteString(h, req.Method+"|"+req.URL.String())
	return "cache:" + hex.EncodeToString(h.Sum(nil))
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   *bodyBuffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.body == nil {
		r.body = &bodyBuffer{}
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

type bodyBuffer struct {
	data []byte
}

func (b *bodyBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bodyBuffer) String() string {
	return string(b.data)
}
