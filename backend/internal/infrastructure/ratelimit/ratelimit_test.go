package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisTokenBucket(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()

	now := time.Unix(1_800_000_000, 0)
	l := NewRedis(rdb, "rl:", Rate{Per: 10, Interval: time.Second, Burst: 3})
	l.now = func() time.Time { return now }

	for i := range 3 {
		if w, err := l.Reserve(ctx, "a"); err != nil || w != 0 {
			t.Fatalf("token %d: wait %v err %v", i, w, err)
		}
	}
	w, err := l.Reserve(ctx, "a")
	if err != nil || w != 100*time.Millisecond {
		t.Fatalf("empty bucket: wait %v err %v", w, err)
	}
	if w, _ := l.Reserve(ctx, "b"); w != 0 {
		t.Fatal("different keys share the bucket")
	}

	now = now.Add(100 * time.Millisecond)
	if w, _ := l.Reserve(ctx, "a"); w != 0 {
		t.Fatalf("after refill: wait %v", w)
	}
}

type fixed struct{ waits []time.Duration }

func (f *fixed) Reserve(context.Context, string) (time.Duration, error) {
	w := f.waits[0]
	f.waits = f.waits[1:]
	return w, nil
}

func TestWait(t *testing.T) {
	ctx := context.Background()
	if err := Wait(ctx, &fixed{[]time.Duration{time.Millisecond, 0}}, "x", time.Second); err != nil {
		t.Fatal(err)
	}
	var lim *ErrLimited
	if err := Wait(ctx, &fixed{[]time.Duration{time.Hour}}, "x", time.Second); !errors.As(err, &lim) {
		t.Fatalf("err %v", err)
	}
}
