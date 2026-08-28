// Package cache provides a Redis-backed HTTP response cache for hot,
// read-heavy GET endpoints (article list/detail, tags, profiles).
//
// It is deliberately optional: if REDIS_ADDR is not set, or Redis is
// unreachable at startup, caching is disabled and Middleware becomes a
// no-op pass-through. A cache outage should degrade performance, never
// availability - the API must keep serving correct answers straight from
// Postgres either way.
package cache

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	client  *redis.Client
	enabled bool
)

// Init connects to Redis using REDIS_ADDR (and optional REDIS_PASSWORD).
// Call once at startup, after flags/env are available and before the
// router starts serving traffic.
func Init() {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		log.Println("cache: REDIS_ADDR not set, response caching disabled")
		enabled = false
		return
	}

	client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		// Fail open: a misconfigured/unreachable cache should never take
		// the API down with it.
		log.Printf("cache: redis at %s unreachable, response caching disabled: %v", addr, err)
		enabled = false
		return
	}

	log.Printf("cache: connected to redis at %s", addr)
	enabled = true
}

// Enabled reports whether Redis caching is active.
func Enabled() bool {
	return enabled
}
