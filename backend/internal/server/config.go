package server

import (
	"fmt"
	"time"

	"github.com/rafixcs/app-parceiros/backend/pkg/env"
)

// Config is the process configuration, read from environment variables.
type Config struct {
	Env             string
	HTTPAddr        string
	DatabaseURL     string
	RedisURL        string
	ShutdownTimeout time.Duration

	// AppURL is the web app address, used in the links sent by email.
	AppURL string

	// Authentication. AuthProvider "oidc" (default) validates tokens from the
	// external provider (Zitadel); "internal" signs users in with email and
	// password in the app itself; "dev" accepts `dev:<sub>` tokens and only
	// works with APP_ENV=dev.
	AuthProvider    string
	OIDCIssuer      string
	OIDCAudience    string
	OIDCJWKSURL     string
	OIDCUserinfoURL string

	// Master key (KEK) that encrypts the data keys of user secrets: 32 bytes
	// in base64. Its ID is stored with each encrypted secret.
	CryptoKEK   string
	CryptoKEKID string

	// Shopee. ShopeeMode "api" talks to the Open API; "mock" replays the
	// recorded answers (only with APP_ENV=dev). The default is "mock" in dev
	// without SHOPEE_APP_ID and "api" elsewhere. The app credential feeds the
	// catalog.
	ShopeeMode        string
	ShopeeURL         string
	ShopeeAppID       string
	ShopeeAppSecret   string
	ShopeeRatePerHour int // calls per hour per credential
	ShopeePages       int // pages of 50 items per category in each snapshot

	// S3 bucket (SeaweedFS locally). Without S3_ENDPOINT, the raw Shopee
	// answers are not kept and there are no video uploads.
	// S3_PUBLIC_ENDPOINT signs the URLs the browser uses, when it cannot
	// reach S3_ENDPOINT (locally, http://localhost:8333).
	S3Endpoint       string
	S3PublicEndpoint string
	S3Bucket         string
	S3AccessKey      string
	S3SecretKey      string
	S3Region         string

	// Email over SMTP (Mailpit locally). Without SMTP_ADDR, no email is sent.
	SMTPAddr     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string

	// Billing. BillingMode "asaas" talks to Asaas; "mock" charges nothing and
	// is the default in dev. ASAAS_URL points to the sandbox to test against
	// the real API.
	BillingMode        string
	AsaasURL           string
	AsaasAPIKey        string
	AsaasWebhookSecret string

	// Web Push: VAPID key pair (generate with `parceiros vapid`) and the
	// contact that goes in the token. Without the keys, push is off.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
}

// LoadConfig reads and validates the configuration.
func LoadConfig() (Config, error) {
	c := Config{
		Env:                env.String("APP_ENV", "dev"),
		HTTPAddr:           env.String("HTTP_ADDR", ":8080"),
		DatabaseURL:        env.String("DATABASE_URL", ""),
		RedisURL:           env.String("REDIS_URL", ""),
		ShutdownTimeout:    15 * time.Second,
		AppURL:             env.String("APP_URL", "http://localhost:5173"),
		AuthProvider:       env.String("AUTH_PROVIDER", "oidc"),
		OIDCIssuer:         env.String("OIDC_ISSUER", ""),
		OIDCAudience:       env.String("OIDC_AUDIENCE", ""),
		OIDCJWKSURL:        env.String("OIDC_JWKS_URL", ""),
		OIDCUserinfoURL:    env.String("OIDC_USERINFO_URL", ""),
		CryptoKEK:          env.String("CRYPTO_KEK", ""),
		CryptoKEKID:        env.String("CRYPTO_KEK_ID", "local-1"),
		ShopeeMode:         env.String("SHOPEE_MODE", ""),
		ShopeeURL:          env.String("SHOPEE_URL", ""),
		ShopeeAppID:        env.String("SHOPEE_APP_ID", ""),
		ShopeeAppSecret:    env.String("SHOPEE_APP_SECRET", ""),
		S3Endpoint:         env.String("S3_ENDPOINT", ""),
		S3PublicEndpoint:   env.String("S3_PUBLIC_ENDPOINT", ""),
		S3Bucket:           env.String("S3_BUCKET", "parceiros"),
		S3AccessKey:        env.String("S3_ACCESS_KEY", ""),
		S3SecretKey:        env.String("S3_SECRET_KEY", ""),
		S3Region:           env.String("S3_REGION", ""),
		SMTPAddr:           env.String("SMTP_ADDR", ""),
		SMTPUsername:       env.String("SMTP_USERNAME", ""),
		SMTPPassword:       env.String("SMTP_PASSWORD", ""),
		SMTPFrom:           env.String("SMTP_FROM", "App Parceiros <nao-responda@parceiros.local>"),
		BillingMode:        env.String("BILLING_MODE", ""),
		AsaasURL:           env.String("ASAAS_URL", ""),
		AsaasAPIKey:        env.String("ASAAS_API_KEY", ""),
		AsaasWebhookSecret: env.String("ASAAS_WEBHOOK_SECRET", ""),
		VAPIDPublicKey:     env.String("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:    env.String("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:       env.String("VAPID_SUBJECT", "contato@parceiros.local"),
	}
	var err error
	if c.ShopeeRatePerHour, err = env.PositiveInt("SHOPEE_RATE_PER_HOUR", 1800); err != nil {
		return c, err
	}
	if c.ShopeePages, err = env.PositiveInt("SHOPEE_PAGES", 10); err != nil {
		return c, err
	}
	switch c.AuthProvider {
	case "oidc", "internal":
	case "dev":
		if c.Env != "dev" {
			return c, fmt.Errorf("AUTH_PROVIDER=dev is only allowed with APP_ENV=dev")
		}
	default:
		return c, fmt.Errorf("AUTH_PROVIDER must be oidc, internal or dev")
	}
	if c.ShopeeMode == "" {
		c.ShopeeMode = "api"
		if c.Env == "dev" && c.ShopeeAppID == "" {
			c.ShopeeMode = "mock"
		}
	}
	switch {
	case c.ShopeeMode != "api" && c.ShopeeMode != "mock":
		return c, fmt.Errorf("SHOPEE_MODE must be api or mock")
	case c.ShopeeMode == "mock" && c.Env != "dev":
		return c, fmt.Errorf("SHOPEE_MODE=mock is only allowed with APP_ENV=dev")
	}
	if c.BillingMode == "" {
		c.BillingMode = "asaas"
		if c.Env == "dev" && c.AsaasAPIKey == "" {
			c.BillingMode = "mock"
		}
	}
	switch {
	case c.BillingMode != "asaas" && c.BillingMode != "mock":
		return c, fmt.Errorf("BILLING_MODE must be asaas or mock")
	case c.BillingMode == "mock" && c.Env != "dev":
		return c, fmt.Errorf("BILLING_MODE=mock is only allowed with APP_ENV=dev")
	case c.BillingMode == "asaas" && c.AsaasAPIKey == "":
		return c, fmt.Errorf("ASAAS_API_KEY is required with BILLING_MODE=asaas")
	}
	if (c.VAPIDPublicKey == "") != (c.VAPIDPrivateKey == "") {
		return c, fmt.Errorf("set VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY together")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.RedisURL == "" {
		return c, fmt.Errorf("REDIS_URL is required")
	}
	return c, nil
}
