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

	agora := time.Unix(1_800_000_000, 0)
	l := NovoRedis(rdb, "rl:", Taxa{Por: 10, Intervalo: time.Second, Rajada: 3})
	l.agora = func() time.Time { return agora }

	for i := range 3 {
		if e, err := l.Reservar(ctx, "a"); err != nil || e != 0 {
			t.Fatalf("ficha %d: espera %v err %v", i, e, err)
		}
	}
	e, err := l.Reservar(ctx, "a")
	if err != nil || e != 100*time.Millisecond {
		t.Fatalf("balde vazio: espera %v err %v", e, err)
	}
	if e, _ := l.Reservar(ctx, "b"); e != 0 {
		t.Fatal("chaves diferentes dividem o balde")
	}

	agora = agora.Add(100 * time.Millisecond)
	if e, _ := l.Reservar(ctx, "a"); e != 0 {
		t.Fatalf("depois da reposição: espera %v", e)
	}
}

type fixo struct{ esperas []time.Duration }

func (f *fixo) Reservar(context.Context, string) (time.Duration, error) {
	e := f.esperas[0]
	f.esperas = f.esperas[1:]
	return e, nil
}

func TestEsperar(t *testing.T) {
	ctx := context.Background()
	if err := Esperar(ctx, &fixo{[]time.Duration{time.Millisecond, 0}}, "x", time.Second); err != nil {
		t.Fatal(err)
	}
	var lim *ErrLimite
	if err := Esperar(ctx, &fixo{[]time.Duration{time.Hour}}, "x", time.Second); !errors.As(err, &lim) {
		t.Fatalf("err %v", err)
	}
}
