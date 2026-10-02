// Package ratelimit limita chamadas a APIs externas com um token bucket no
// Redis, compartilhado entre todas as réplicas da API e do worker.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Taxa é a capacidade de um balde: Rajada fichas, repostas a Por por Intervalo.
type Taxa struct {
	Por       int
	Intervalo time.Duration
	Rajada    int
}

// Limitador reserva fichas de um balde por chave (ex.: a credencial).
type Limitador interface {
	// Reservar tira uma ficha do balde da chave. Se não houver, devolve
	// quanto esperar antes de tentar de novo, sem consumir nada.
	Reservar(ctx context.Context, chave string) (time.Duration, error)
}

// Esperar reserva uma ficha, dormindo o necessário. Desiste quando a espera
// passa de max ou o contexto acaba.
func Esperar(ctx context.Context, l Limitador, chave string, max time.Duration) error {
	limite := time.Now().Add(max)
	for {
		espera, err := l.Reservar(ctx, chave)
		if err != nil {
			return err
		}
		if espera == 0 {
			return nil
		}
		if time.Now().Add(espera).After(limite) {
			return &ErrLimite{Espera: espera}
		}
		t := time.NewTimer(espera)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// ErrLimite indica que o balde está vazio por mais tempo do que dá para esperar.
type ErrLimite struct{ Espera time.Duration }

func (e *ErrLimite) Error() string {
	return fmt.Sprintf("limite de chamadas atingido; tente em %s", e.Espera.Round(time.Second))
}

// Redis implementa o Limitador com um script Lua atômico.
type Redis struct {
	rdb    redis.Scripter
	taxa   Taxa
	prefix string
	agora  func() time.Time
}

func NovoRedis(rdb redis.Scripter, prefix string, t Taxa) *Redis {
	if t.Rajada < 1 {
		t.Rajada = 1
	}
	return &Redis{rdb: rdb, taxa: t, prefix: prefix, agora: time.Now}
}

// Estado do balde: fichas e o instante (ms) da última reposição.
var script = redis.NewScript(`
local capacidade = tonumber(ARGV[1])
local por_ms = tonumber(ARGV[2])
local agora = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local b = redis.call('HMGET', KEYS[1], 'fichas', 'ts')
local fichas = tonumber(b[1])
local ts = tonumber(b[2])
if fichas == nil then
  fichas = capacidade
  ts = agora
end
if agora > ts then
  fichas = math.min(capacidade, fichas + (agora - ts) * por_ms)
  ts = agora
end
local espera = 0
if fichas >= 1 then
  fichas = fichas - 1
else
  espera = math.ceil((1 - fichas) / por_ms)
end
redis.call('HSET', KEYS[1], 'fichas', tostring(fichas), 'ts', ts)
redis.call('PEXPIRE', KEYS[1], ttl)
return espera
`)

func (r *Redis) Reservar(ctx context.Context, chave string) (time.Duration, error) {
	porMs := float64(r.taxa.Por) / float64(r.taxa.Intervalo.Milliseconds())
	// O balde some do Redis depois de ficar cheio de novo.
	ttl := int64(float64(r.taxa.Rajada)/porMs) + 1000
	ms, err := script.Run(ctx, r.rdb, []string{r.prefix + chave},
		r.taxa.Rajada, porMs, r.agora().UnixMilli(), ttl).Int64()
	if err != nil {
		return 0, fmt.Errorf("rate limit no redis: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// Livre não limita nada. Serve para testes e para o mock da Shopee.
type Livre struct{}

func (Livre) Reservar(context.Context, string) (time.Duration, error) { return 0, nil }
