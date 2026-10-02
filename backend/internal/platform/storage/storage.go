// Package storage guarda objetos num bucket compatível com S3: Cloudflare R2
// em produção e SeaweedFS no ambiente local.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage guarda objetos por chave.
type Storage interface {
	Guardar(ctx context.Context, chave string, dados []byte, contentType string) error
}

type Config struct {
	Endpoint string // ex.: http://seaweedfs:8333 ou https://<conta>.r2.cloudflarestorage.com
	// EndpointPublico assina as URLs que o navegador usa (upload e download),
	// quando ele não alcança o Endpoint (no ambiente local, o S3 do cluster
	// responde em http://localhost:8333). Vazio usa o Endpoint.
	EndpointPublico string
	Bucket          string
	AccessKey       string
	SecretKey       string
	Region          string
}

// S3 é o Storage num bucket compatível com S3.
type S3 struct {
	cli    *minio.Client
	pub    *minio.Client // só assina URLs para o navegador
	bucket string
}

func NovoS3(c Config) (*S3, error) {
	cli, err := novoCliente(c, c.Endpoint, "S3_ENDPOINT")
	if err != nil {
		return nil, err
	}
	pub := cli
	if c.EndpointPublico != "" {
		if pub, err = novoCliente(c, c.EndpointPublico, "S3_ENDPOINT_PUBLICO"); err != nil {
			return nil, err
		}
	}
	return &S3{cli: cli, pub: pub, bucket: c.Bucket}, nil
}

func novoCliente(c Config, endpoint, nome string) (*minio.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%s inválido %q", nome, endpoint)
	}
	region := c.Region
	if region == "" {
		region = "auto"
	}
	// Com a região fixa, assinar uma URL não faz chamada de rede.
	return minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: u.Scheme == "https",
		Region: region,
	})
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

// ErrNaoEncontrado indica que o objeto (ou o upload multipart) não existe.
var ErrNaoEncontrado = errors.New("objeto não encontrado")

func traduzir(err error) error {
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchUpload":
		return fmt.Errorf("%w: %w", ErrNaoEncontrado, err)
	}
	return err
}

// Parte é uma parte de um upload multipart.
type Parte struct {
	Numero  int
	ETag    string
	Tamanho int64
}

// Objeto descreve um objeto guardado.
type Objeto struct {
	Tamanho     int64
	ContentType string
}

// IniciarMultipart abre um upload multipart e devolve o uploadId. As partes
// vão direto do navegador ao bucket por URLs de AssinarParte.
func (s *S3) IniciarMultipart(ctx context.Context, chave, contentType string) (string, error) {
	id, err := minio.Core{Client: s.cli}.NewMultipartUpload(ctx, s.bucket, chave, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("iniciando upload de %s: %w", chave, err)
	}
	return id, nil
}

// AssinarParte devolve a URL pré-assinada (PUT) de uma parte do upload.
func (s *S3) AssinarParte(ctx context.Context, chave, uploadID string, numero int, validade time.Duration) (string, error) {
	q := url.Values{"partNumber": {strconv.Itoa(numero)}, "uploadId": {uploadID}}
	u, err := s.pub.Presign(ctx, http.MethodPut, s.bucket, chave, validade, q)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Partes lista as partes já enviadas de um upload.
func (s *S3) Partes(ctx context.Context, chave, uploadID string) ([]Parte, error) {
	core := minio.Core{Client: s.cli}
	var out []Parte
	marcador := 0
	for {
		r, err := core.ListObjectParts(ctx, s.bucket, chave, uploadID, marcador, 1000)
		if err != nil {
			return nil, traduzir(err)
		}
		for _, p := range r.ObjectParts {
			out = append(out, Parte{Numero: p.PartNumber, ETag: p.ETag, Tamanho: p.Size})
		}
		if !r.IsTruncated {
			return out, nil
		}
		marcador = r.NextPartNumberMarker
	}
}

// ConcluirMultipart junta as partes no objeto final.
func (s *S3) ConcluirMultipart(ctx context.Context, chave, uploadID string, partes []Parte) error {
	cp := make([]minio.CompletePart, len(partes))
	for i, p := range partes {
		cp[i] = minio.CompletePart{PartNumber: p.Numero, ETag: p.ETag}
	}
	_, err := minio.Core{Client: s.cli}.CompleteMultipartUpload(ctx, s.bucket, chave, uploadID, cp, minio.PutObjectOptions{})
	return traduzir(err)
}

// AbortarMultipart descarta um upload e as partes já enviadas. Um upload
// que não existe mais não é erro.
func (s *S3) AbortarMultipart(ctx context.Context, chave, uploadID string) error {
	err := traduzir(minio.Core{Client: s.cli}.AbortMultipartUpload(ctx, s.bucket, chave, uploadID))
	if errors.Is(err, ErrNaoEncontrado) {
		return nil
	}
	return err
}

// Info devolve tamanho e tipo de um objeto.
func (s *S3) Info(ctx context.Context, chave string) (Objeto, error) {
	st, err := s.cli.StatObject(ctx, s.bucket, chave, minio.StatObjectOptions{})
	if err != nil {
		return Objeto{}, traduzir(err)
	}
	return Objeto{Tamanho: st.Size, ContentType: st.ContentType}, nil
}

// AssinarGet devolve uma URL pré-assinada para o navegador ler o objeto. Com
// baixarComo, a resposta pede ao navegador para salvar com esse nome.
func (s *S3) AssinarGet(ctx context.Context, chave string, validade time.Duration, baixarComo string) (string, error) {
	q := url.Values{}
	if baixarComo != "" {
		q.Set("response-content-disposition", Anexo(baixarComo))
	}
	u, err := s.pub.PresignedGetObject(ctx, s.bucket, chave, validade, q)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// URLInterna devolve uma URL pré-assinada de leitura pelo endpoint interno,
// para processos do cluster (o ffmpeg do worker).
func (s *S3) URLInterna(ctx context.Context, chave string, validade time.Duration) (string, error) {
	u, err := s.cli.PresignedGetObject(ctx, s.bucket, chave, validade, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// EnviarArquivo guarda um arquivo local.
func (s *S3) EnviarArquivo(ctx context.Context, chave, caminho, contentType string) error {
	if _, err := s.cli.FPutObject(ctx, s.bucket, chave, caminho, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("guardando %s: %w", chave, err)
	}
	return nil
}

// ApagarPrefixo apaga todos os objetos que começam com prefixo.
func (s *S3) ApagarPrefixo(ctx context.Context, prefixo string) error {
	objs := make(chan minio.ObjectInfo)
	errs := make(chan error, 1)
	go func() {
		defer close(objs)
		for o := range s.cli.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefixo, Recursive: true}) {
			if o.Err != nil {
				errs <- o.Err
				return
			}
			objs <- o
		}
	}()
	for e := range s.cli.RemoveObjects(ctx, s.bucket, objs, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return fmt.Errorf("apagando %s: %w", e.ObjectName, e.Err)
		}
	}
	select {
	case err := <-errs:
		return fmt.Errorf("listando %s: %w", prefixo, err)
	default:
		return nil
	}
}

// Anexo monta o Content-Disposition de download com o nome do arquivo,
// também em UTF-8 (RFC 6266) para nomes com acento.
func Anexo(nome string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, nome)
	var b strings.Builder
	for _, c := range []byte(nome) {
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, b.String())
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
