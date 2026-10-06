// Package ratelimit throttles calls with a token bucket in Redis, shared by
// every API and worker replica.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rate is the capacity of a bucket: Burst tokens, refilled at Per per Interval.
type Rate struct {
	Per      int
	Interval time.Duration
	Burst    int
}

// Limiter reserves tokens from one bucket per key (e.g. the credential).
type Limiter interface {
	// Reserve takes a token from the key's bucket. When it is empty, it
	// returns how long to wait before trying again, consuming nothing.
	Reserve(ctx context.Context, key string) (time.Duration, error)
}

// Wait reserves a token, sleeping as needed. It gives up when the wait goes
// beyond limit or the context ends.
func Wait(ctx context.Context, l Limiter, key string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		wait, err := l.Reserve(ctx, key)
		if err != nil {
			return err
		}
		if wait == 0 {
			return nil
		}
		if time.Now().Add(wait).After(deadline) {
			return &ErrLimited{Wait: wait}
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// ErrLimited means the bucket stays empty for longer than the caller can wait.
type ErrLimited struct{ Wait time.Duration }

func (e *ErrLimited) Error() string {
	return fmt.Sprintf("rate limit reached; retry in %s", e.Wait.Round(time.Second))
}

// Redis implements Limiter with an atomic Lua script.
type Redis struct {
	rdb    redis.Scripter
	rate   Rate
	prefix string
	now    func() time.Time
}

func NewRedis(rdb redis.Scripter, prefix string, r Rate) *Redis {
	if r.Burst < 1 {
		r.Burst = 1
	}
	return &Redis{rdb: rdb, rate: r, prefix: prefix, now: time.Now}
}

// Bucket state: tokens and the instant (ms) of the last refill.
var script = redis.NewScript(`
local capacity = tonumber(ARGV[1])
local per_ms = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local b = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(b[1])
local ts = tonumber(b[2])
if tokens == nil then
  tokens = capacity
  ts = now
end
if now > ts then
  tokens = math.min(capacity, tokens + (now - ts) * per_ms)
  ts = now
end
local wait = 0
if tokens >= 1 then
  tokens = tokens - 1
else
  wait = math.ceil((1 - tokens) / per_ms)
end
redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', ts)
redis.call('PEXPIRE', KEYS[1], ttl)
return wait
`)

func (r *Redis) Reserve(ctx context.Context, key string) (time.Duration, error) {
	perMs := float64(r.rate.Per) / float64(r.rate.Interval.Milliseconds())
	// The bucket leaves Redis once it would be full again.
	ttl := int64(float64(r.rate.Burst)/perMs) + 1000
	ms, err := script.Run(ctx, r.rdb, []string{r.prefix + key},
		r.rate.Burst, perMs, r.now().UnixMilli(), ttl).Int64()
	if err != nil {
		return 0, fmt.Errorf("redis rate limit: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// Unlimited limits nothing. Meant for tests and the Shopee mock.
type Unlimited struct{}

func (Unlimited) Reserve(context.Context, string) (time.Duration, error) { return 0, nil }
