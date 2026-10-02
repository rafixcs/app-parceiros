// Package config carrega a configuração do processo a partir de variáveis de ambiente.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Env             string
	HTTPAddr        string
	DatabaseURL     string
	RedisURL        string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	c := Config{
		Env:             getenv("APP_ENV", "dev"),
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		ShutdownTimeout: 15 * time.Second,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL é obrigatória")
	}
	if c.RedisURL == "" {
		return c, fmt.Errorf("REDIS_URL é obrigatória")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
