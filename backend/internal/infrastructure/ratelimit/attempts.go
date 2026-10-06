package ratelimit

import "context"

// Attempts adapts a Limiter to domain.RateLimiter: an attempt is allowed when
// a token is available right now, and refused (never delayed) otherwise.
type Attempts struct {
	Limiter Limiter
}

func (a Attempts) Allow(ctx context.Context, key string) (bool, error) {
	wait, err := a.Limiter.Reserve(ctx, key)
	if err != nil {
		return false, err
	}
	return wait == 0, nil
}
