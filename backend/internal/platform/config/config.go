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

	// AppURL é o endereço do front, usado nos links de convite.
	AppURL string

	// Autenticação. AuthMode "oidc" (padrão) valida tokens do Zitadel; "dev"
	// aceita tokens `dev:<sub>` e só funciona com APP_ENV=dev.
	AuthMode        string
	OIDCIssuer      string
	OIDCAudience    string
	OIDCJWKSURL     string
	OIDCUserinfoURL string
}

func Load() (Config, error) {
	c := Config{
		Env:             getenv("APP_ENV", "dev"),
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		ShutdownTimeout: 15 * time.Second,
		AppURL:          getenv("APP_URL", "http://localhost:5173"),
		AuthMode:        getenv("AUTH_MODE", "oidc"),
		OIDCIssuer:      os.Getenv("OIDC_ISSUER"),
		OIDCAudience:    os.Getenv("OIDC_AUDIENCE"),
		OIDCJWKSURL:     os.Getenv("OIDC_JWKS_URL"),
		OIDCUserinfoURL: os.Getenv("OIDC_USERINFO_URL"),
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
