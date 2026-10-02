// Package storage guarda objetos num bucket compatível com S3: Cloudflare R2
// em produção e SeaweedFS no ambiente local.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage guarda objetos por chave.
type Storage interface {
	Guardar(ctx context.Context, chave string, dados []byte, contentType string) error
}

type Config struct {
	Endpoint  string // ex.: http://seaweedfs:8333 ou https://<conta>.r2.cloudflarestorage.com
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
}

// S3 é o Storage num bucket compatível com S3.
type S3 struct {
	cli    *minio.Client
	bucket string
}

func NovoS3(c Config) (*S3, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("S3_ENDPOINT inválido %q", c.Endpoint)
	}
	region := c.Region
	if region == "" {
		region = "auto"
	}
	cli, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: u.Scheme == "https",
		Region: region,
	})
	if err != nil {
		return nil, err
	}
	return &S3{cli: cli, bucket: c.Bucket}, nil
}

// GarantirBucket cria o bucket se ele não existir. Útil no ambiente local; em
// produção o bucket é criado pela infra.
func (s *S3) GarantirBucket(ctx context.Context) error {
	ok, err := s.cli.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("verificando bucket %s: %w", s.bucket, err)
	}
	if ok {
		return nil
	}
	return s.cli.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}

func (s *S3) Guardar(ctx context.Context, chave string, dados []byte, contentType string) error {
	_, err := s.cli.PutObject(ctx, s.bucket, chave, bytes.NewReader(dados), int64(len(dados)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("guardando %s: %w", chave, err)
	}
	return nil
}

// Memoria guarda os objetos num mapa. Serve para testes.
type Memoria struct {
	mu      sync.Mutex
	Objetos map[string][]byte
}

func (m *Memoria) Guardar(_ context.Context, chave string, dados []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Objetos == nil {
		m.Objetos = map[string][]byte{}
	}
	m.Objetos[chave] = append([]byte(nil), dados...)
	return nil
}

func (m *Memoria) Chaves() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.Objetos))
	for k := range m.Objetos {
		out = append(out, k)
	}
	return out
}

// Descartar não guarda nada. Usado quando não há bucket configurado.
type Descartar struct{}

func (Descartar) Guardar(context.Context, string, []byte, string) error { return nil }
