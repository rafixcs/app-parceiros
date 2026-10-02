// Package config carrega a configuração do processo a partir de variáveis de ambiente.
package config

import (
	"fmt"
	"os"
	"strconv"
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

	// Chave mestra (KEK) que cifra as chaves de dados dos segredos de
	// usuário: 32 bytes em base64. O ID vai junto de cada segredo cifrado.
	CryptoKEK   string
	CryptoKEKID string

	// Shopee. ShopeeModo "api" fala com a Open API; "mock" usa as respostas
	// gravadas (só com APP_ENV=dev). O padrão é "mock" em dev sem
	// SHOPEE_APP_ID e "api" no resto. A credencial do app alimenta o catálogo.
	ShopeeModo        string
	ShopeeURL         string
	ShopeeAppID       string
	ShopeeAppSecret   string
	ShopeeRatePorHora int // chamadas por hora por credencial
	ShopeePaginas     int // páginas de 50 itens por categoria em cada snapshot

	// Bucket S3 (R2 em produção, MinIO local). Sem S3_ENDPOINT, as respostas
	// brutas da Shopee não são guardadas.
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
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
		CryptoKEK:       os.Getenv("CRYPTO_KEK"),
		CryptoKEKID:     getenv("CRYPTO_KEK_ID", "local-1"),
		ShopeeModo:      os.Getenv("SHOPEE_MODO"),
		ShopeeURL:       os.Getenv("SHOPEE_URL"),
		ShopeeAppID:     os.Getenv("SHOPEE_APP_ID"),
		ShopeeAppSecret: os.Getenv("SHOPEE_APP_SECRET"),
		S3Endpoint:      os.Getenv("S3_ENDPOINT"),
		S3Bucket:        getenv("S3_BUCKET", "parceiros"),
		S3AccessKey:     os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:     os.Getenv("S3_SECRET_KEY"),
		S3Region:        os.Getenv("S3_REGION"),
	}
	var err error
	if c.ShopeeRatePorHora, err = getint("SHOPEE_RATE_POR_HORA", 1800); err != nil {
		return c, err
	}
	if c.ShopeePaginas, err = getint("SHOPEE_PAGINAS", 10); err != nil {
		return c, err
	}
	if c.ShopeeModo == "" {
		c.ShopeeModo = "api"
		if c.Env == "dev" && c.ShopeeAppID == "" {
			c.ShopeeModo = "mock"
		}
	}
	switch {
	case c.ShopeeModo != "api" && c.ShopeeModo != "mock":
		return c, fmt.Errorf("SHOPEE_MODO deve ser api ou mock")
	case c.ShopeeModo == "mock" && c.Env != "dev":
		return c, fmt.Errorf("SHOPEE_MODO=mock só é aceito com APP_ENV=dev")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL é obrigatória")
	}
	if c.RedisURL == "" {
		return c, fmt.Errorf("REDIS_URL é obrigatória")
	}
	return c, nil
}

func getint(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s deve ser um inteiro positivo", key)
	}
	return n, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
