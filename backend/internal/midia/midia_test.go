package midia_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/midia"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

const (
	youtubeOK   = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	youtubeOK2  = "https://youtu.be/aaaaaaaaaaa"
	tiktokCurto = "https://vm.tiktok.com/ZMabc123/"
	idTikTok    = "7350000000000000001"
)

// bucket é um S3 em memória com upload multipart. As partes chegam por
// receber, no lugar do PUT do navegador.
type bucket struct {
	mu      sync.Mutex
	dir     string
	objetos map[string][]byte
	uploads map[string]*upload
	seq     int
}

type upload struct {
	chave  string
	partes map[int][]byte
}

func novoBucket(t *testing.T) *bucket {
	return &bucket{dir: t.TempDir(), objetos: map[string][]byte{}, uploads: map[string]*upload{}}
}

func etag(dados []byte) string { return fmt.Sprintf(`"%x"`, len(dados)*31+int(dados[0])) }

func (b *bucket) StartMultipart(_ context.Context, chave, _ string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := fmt.Sprintf("up-%d", b.seq)
	b.uploads[id] = &upload{chave: chave, partes: map[int][]byte{}}
	return id, nil
}

func (b *bucket) SignPart(_ context.Context, chave, uploadID string, numero int, _ time.Duration) (string, error) {
	return fmt.Sprintf("https://s3.teste/%s?partNumber=%d&uploadId=%s", chave, numero, uploadID), nil
}

func (b *bucket) receber(t *testing.T, urlParte string, dados []byte) storage.Part {
	t.Helper()
	u, err := url.Parse(urlParte)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	up, ok := b.uploads[u.Query().Get("uploadId")]
	if !ok || "/"+up.chave != u.Path {
		t.Fatalf("parte para upload desconhecido: %s", urlParte)
	}
	var n int
	_, _ = fmt.Sscan(u.Query().Get("partNumber"), &n)
	up.partes[n] = dados
	return storage.Part{Number: n, ETag: etag(dados)}
}

func (b *bucket) Parts(_ context.Context, _, uploadID string) ([]storage.Part, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	up, ok := b.uploads[uploadID]
	if !ok {
		return nil, storage.ErrNotFound
	}
	var out []storage.Part
	for n, d := range up.partes {
		out = append(out, storage.Part{Number: n, ETag: etag(d), Size: int64(len(d))})
	}
	slices.SortFunc(out, func(a, b storage.Part) int { return a.Number - b.Number })
	return out, nil
}

func (b *bucket) CompleteMultipart(_ context.Context, chave, uploadID string, partes []storage.Part) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	up, ok := b.uploads[uploadID]
	if !ok {
		return storage.ErrNotFound
	}
	var tudo []byte
	for _, p := range partes {
		d, ok := up.partes[p.Number]
		if !ok || etag(d) != p.ETag {
			return fmt.Errorf("InvalidPart %d", p.Number)
		}
		tudo = append(tudo, d...)
	}
	b.objetos[chave] = tudo
	delete(b.uploads, uploadID)
	return nil
}

func (b *bucket) AbortMultipart(_ context.Context, _, uploadID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.uploads, uploadID)
	return nil
}

func (b *bucket) Info(_ context.Context, chave string) (storage.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.objetos[chave]
	if !ok {
		return storage.Object{}, storage.ErrNotFound
	}
	return storage.Object{Size: int64(len(d))}, nil
}

func (b *bucket) SignGet(_ context.Context, chave string, _ time.Duration, baixarComo string) (string, error) {
	u := "https://s3.teste/" + chave
	if baixarComo != "" {
		u += "?baixar=" + url.QueryEscape(baixarComo)
	}
	return u, nil
}

func (b *bucket) InternalURL(_ context.Context, chave string, _ time.Duration) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := filepath.Join(b.dir, strings.ReplaceAll(chave, "/", "_"))
	return f, os.WriteFile(f, b.objetos[chave], 0o600)
}

func (b *bucket) UploadFile(_ context.Context, chave, caminho, _ string) error {
	d, err := os.ReadFile(caminho)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objetos[chave] = d
	return nil
}

func (b *bucket) DeletePrefix(_ context.Context, prefixo string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.objetos {
		if strings.HasPrefix(k, prefixo) {
			delete(b.objetos, k)
		}
	}
	return nil
}

func (b *bucket) chaves(prefixo string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for k := range b.objetos {
		if strings.HasPrefix(k, prefixo) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (b *bucket) abertos() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.uploads)
}

// processador finge o ffmpeg: grava prévia e miniatura no diretório.
type processador struct{ recusar bool }

func (p processador) Processar(_ context.Context, entrada, dir string) (midia.Processado, error) {
	if _, err := os.Stat(entrada); err != nil {
		return midia.Processado{}, err
	}
	if p.recusar {
		return midia.Processado{}, midia.ErrNaoEhVideo
	}
	out := midia.Processado{DuracaoS: 12, Largura: 720, Altura: 1280, Previa: filepath.Join(dir, "p.mp4"), Thumb: filepath.Join(dir, "t.jpg")}
	if err := os.WriteFile(out.Previa, []byte("previa"), 0o600); err != nil {
		return out, err
	}
	return out, os.WriteFile(out.Thumb, []byte("thumb"), 0o600)
}

// oembed responde como os endpoints oficiais do YouTube e do TikTok.
type oembed struct {
	mu        sync.Mutex
	removidos map[string]bool
	chamadas  int
}

func (o *oembed) remover(link string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.removidos[link] = true
}

func (o *oembed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.chamadas++
	removido := o.removidos[r.URL.Query().Get("url")]
	o.mu.Unlock()
	link := r.URL.Query().Get("url")
	if removido {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/youtube":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title": "Review do " + link[len(link)-11:], "author_name": "Canal Achados",
			"thumbnail_url": "https://i.ytimg.com/vi/" + link[len(link)-11:] + "/hqdefault.jpg",
		})
	case "/tiktok":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title": "Unboxing #achadinhos", "author_name": "Criadora", "author_unique_id": "criadora",
			"thumbnail_url": "https://p16-sign.tiktokcdn.com/thumb.jpeg", "embed_product_id": idTikTok,
		})
	default:
		http.NotFound(w, r)
	}
}

type fila struct {
	mu   sync.Mutex
	jobs []midia.Job
}

func (f *fila) Enfileirar(_ context.Context, js ...midia.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, js...)
	return nil
}

func (f *fila) tirar() []midia.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.jobs
	f.jobs = nil
	return out
}

type ambiente struct {
	t        *testing.T
	pool     *pgxpool.Pool
	router   http.Handler
	bucket   *bucket
	oembed   *oembed
	fila     *fila
	worker   *midia.Service
	produtos *produtos.Service
	ofertas  []fontes.Oferta
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	cliente := shopee.NovoMock(&shopee.Mock{}, shopee.Config{})
	app := shopee.CatalogoDoApp{Cliente: cliente, Credencial: shopee.Credencial{AppID: "1", Secret: "x"}}
	produtosSvc := produtos.NewService(pool)
	pg, err := app.Ofertas(ctx, fontes.FiltroCatalogo{Pagina: 1, Limite: 50})
	if err != nil {
		t.Fatal(err)
	}
	if err := produtosSvc.Registrar(ctx, fontes.Shopee, time.Now(), pg.Ofertas[:5]); err != nil {
		t.Fatal(err)
	}

	oe := &oembed{removidos: map[string]bool{}}
	srv := httptest.NewServer(oe)
	t.Cleanup(srv.Close)
	resolver := &midia.OEmbed{HTTP: srv.Client(), YouTubeURL: srv.URL + "/youtube", TikTokURL: srv.URL + "/tiktok"}

	b := novoBucket(t)
	f := &fila{}
	contasSvc := contas.NewService(pool, auth.Dev{}, "https://app.teste")
	midiaSvc := midia.NewService(pool, produtosSvc, resolver, b, nil, contasSvc, f, log)
	colecoesSvc := colecoes.NewService(pool, produtosSvc, app, shopee.Afiliador{Cliente: cliente}, &filaNula{}, log)
	notificacoesSvc := notificacoes.NewService(pool, filaAvisos{}, contasSvc, nil, nil, "https://app.teste", log)
	curadoriaSvc := curadoria.NewService(pool, produtosSvc, colecoesSvc, contasSvc, notificacoesSvc, midiaSvc, log)

	r := httpapi.NewRouter(log, nil)
	contas.NewHandler(contasSvc, log).Rotas(r, auth.Dev{},
		midia.NewHandler(midiaSvc, log).Modulo(),
		curadoria.NewHandler(curadoriaSvc, log).Modulo(),
	)
	return &ambiente{
		t: t, pool: pool, router: r, bucket: b, oembed: oe, fila: f,
		worker:   midia.NewService(pool, produtosSvc, resolver, b, processador{}, contas.NewService(pool, nil, ""), f, log),
		produtos: produtosSvc, ofertas: pg.Ofertas,
	}
}

type filaNula struct{}

func (filaNula) Enfileirar(context.Context, ...colecoes.GerarLinkArgs) error { return nil }

type filaAvisos struct{}

func (filaAvisos) Enfileirar(context.Context, ...notificacoes.EntregarArgs) error { return nil }

func (a *ambiente) chamar(sub, metodo, caminho string, corpo any, out any) int {
	a.t.Helper()
	var body io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			a.t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req := httptest.NewRequest(metodo, caminho, body)
	req.Header.Set("Authorization", "Bearer dev:"+sub)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s %s: resposta inválida %q: %v", metodo, caminho, rec.Body.String(), err)
		}
	}
	return rec.Code
}

func (a *ambiente) exigir(sub, metodo, caminho string, corpo any, out any, status int) {
	a.t.Helper()
	var bruto json.RawMessage
	if out == nil {
		out = &bruto
	}
	if got := a.chamar(sub, metodo, caminho, corpo, out); got != status {
		a.t.Fatalf("%s %s como %q: status %d, quer %d (%+v)", metodo, caminho, sub, got, status, out)
	}
}

func (a *ambiente) exigirErro(sub, metodo, caminho string, corpo any, status int, codigo string) {
	a.t.Helper()
	var e httputil.ErrorBody
	if got := a.chamar(sub, metodo, caminho, corpo, &e); got != status || e.Code != codigo {
		a.t.Fatalf("%s %s como %q: %d %q, quer %d %q (%s)", metodo, caminho, sub, got, e.Code, status, codigo, e.Message)
	}
}

// pessoal devolve a base das rotas do workspace pessoal do usuário.
func (a *ambiente) pessoal(sub string) string {
	a.t.Helper()
	var ws []contas.Workspace
	a.exigir(sub, http.MethodGet, "/v1/workspaces", nil, &ws, 200)
	for _, w := range ws {
		if w.Tipo == contas.TipoPessoal {
			return "/v1/workspaces/" + w.ID.String()
		}
	}
	a.t.Fatalf("%s sem workspace pessoal", sub)
	return ""
}

// mentoria cria a mentoria do mestre com os afiliados convidados.
func (a *ambiente) mentoria(mestre string, afiliados ...string) string {
	a.t.Helper()
	var ws contas.Workspace
	a.exigir(mestre, http.MethodPost, "/v1/workspaces", map[string]any{"nome": "Turma do " + mestre}, &ws, 201)
	for _, sub := range afiliados {
		var c contas.Convite
		a.exigir(mestre, http.MethodPost, "/v1/workspaces/"+ws.ID.String()+"/convites", map[string]any{}, &c, 201)
		a.exigir(sub, http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, 200)
	}
	return "/v1/workspaces/" + ws.ID.String()
}

func (a *ambiente) produtoID(i int) uuid.UUID {
	a.t.Helper()
	p, err := a.produtos.PorItem(context.Background(), database.Scope{}, fontes.Shopee, a.ofertas[i].ItemID)
	if err != nil {
		a.t.Fatal(err)
	}
	return p.ID
}

// enviar faz o caminho do Uppy: inicia, envia as partes pelas URLs
// assinadas e conclui. Devolve o vídeo em processamento.
func (a *ambiente) enviar(sub, base string, nome string, dados []byte, produtoID *uuid.UUID) midia.Video {
	a.t.Helper()
	var u midia.UploadIniciado
	corpo := map[string]any{"nome": nome, "content_type": "video/mp4", "tamanho": len(dados), "direito_uso": true}
	if produtoID != nil {
		corpo["produto_id"] = produtoID
	}
	a.exigir(sub, http.MethodPost, base+"/videos/uploads", corpo, &u, 201)
	video := base + "/videos/" + u.Video.ID.String()
	var partes []storage.Part
	for i, pedaco := range [][]byte{dados[:len(dados)/2], dados[len(dados)/2:]} {
		var assinada struct{ URL string }
		a.exigir(sub, http.MethodPost, video+"/partes", map[string]any{"numero": i + 1}, &assinada, 200)
		p := a.bucket.receber(a.t, assinada.URL, pedaco)
		partes = append(partes, p)
	}
	var v midia.Video
	a.exigir(sub, http.MethodPost, video+"/concluir", map[string]any{"partes": []map[string]any{
		{"numero": partes[0].Number, "etag": partes[0].ETag}, {"numero": partes[1].Number, "etag": partes[1].ETag},
	}}, &v, 200)
	return v
}

// rodar executa os jobs enfileirados (ou só os de um tipo) com o worker.
func (a *ambiente) rodar(kind string) []midia.Job {
	a.t.Helper()
	var rodados, resto []midia.Job
	for _, j := range a.fila.tirar() {
		if kind != "" && j.Args.Kind() != kind {
			resto = append(resto, j)
			continue
		}
		rodados = append(rodados, j)
		row := &rivertype.JobRow{Kind: j.Args.Kind(), Attempt: 1, MaxAttempts: 3}
		var err error
		ctx := context.Background()
		switch args := j.Args.(type) {
		case midia.ProcessarVideoArgs:
			err = (&midia.ProcessarVideoWorker{Svc: a.worker, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Work(ctx, &river.Job[midia.ProcessarVideoArgs]{JobRow: row, Args: args})
		case midia.RevalidarEmbedArgs:
			err = (&midia.RevalidarEmbedWorker{Svc: a.worker}).Work(ctx, &river.Job[midia.RevalidarEmbedArgs]{JobRow: row, Args: args})
		case midia.LimparUploadArgs:
			err = (&midia.LimparUploadWorker{Svc: a.worker}).Work(ctx, &river.Job[midia.LimparUploadArgs]{JobRow: row, Args: args})
		}
		if err != nil {
			a.t.Fatalf("%s: %v", j.Args.Kind(), err)
		}
	}
	_ = a.fila.Enfileirar(context.Background(), resto...)
	return rodados
}

func ids(vs []midia.Video) []uuid.UUID {
	out := make([]uuid.UUID, len(vs))
	for i, v := range vs {
		out[i] = v.ID
	}
	return out
}

// TestVideosAparecemNoProduto é o critério do M5: um vídeo de referência
// (embed) e um vídeo próprio (upload) aparecem no produto.
func TestVideosAparecemNoProduto(t *testing.T) {
	a := novoAmbiente(t)
	base := a.pessoal("ana")
	produto := a.produtoID(0)

	// Referência: só metadados do oEmbed e o player oficial.
	var ref midia.Video
	a.exigir("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": youtubeOK + "&t=42", "produto_id": produto}, &ref, 201)
	if ref.Tipo != midia.TipoEmbed || ref.Plataforma != "youtube" || ref.Status != midia.StatusPronto ||
		ref.Titulo != "Review do dQw4w9WgXcQ" || ref.Autor != "Canal Achados" || *ref.URL != youtubeOK ||
		*ref.PlayerURL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" || ref.ThumbURL == nil ||
		!ref.Meu || !slices.Equal(ref.ProdutoIDs, []uuid.UUID{produto}) {
		t.Fatalf("referência: %+v", ref)
	}
	// Colar de novo devolve o mesmo vídeo (do cache não há; o oEmbed responde de novo).
	var deNovo midia.Video
	a.exigir("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ"}, &deNovo, 200)
	if deNovo.ID != ref.ID {
		t.Fatalf("colar de novo criou outro vídeo: %s", deNovo.ID)
	}
	// TikTok por link curto: o id e a URL completa vêm do oEmbed.
	var tk midia.Video
	a.exigir("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": tiktokCurto}, &tk, 201)
	if *tk.URL != "https://www.tiktok.com/@criadora/video/"+idTikTok || *tk.PlayerURL != "https://www.tiktok.com/player/v1/"+idTikTok {
		t.Fatalf("tiktok: %+v", tk)
	}
	a.exigirErro("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": "https://vimeo.com/123"}, 422, "link_video_invalido")
	a.oembed.remover("https://www.youtube.com/watch?v=bbbbbbbbbbb")
	a.exigirErro("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": "https://youtu.be/bbbbbbbbbbb"}, 422, "video_indisponivel")
	a.exigirErro("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": youtubeOK, "produto_id": uuid.New()}, 404, "produto_nao_encontrado")

	// Upload próprio: as partes vão direto ao bucket, o worker processa.
	dados := bytes.Repeat([]byte("v"), 1000)
	v := a.enviar("ana", base, "Meu unboxing_final.mp4", dados, &produto)
	if v.Status != midia.StatusProcessando || v.TamanhoBytes != 1000 || v.Titulo != "Meu unboxing final" || v.PreviewURL != nil {
		t.Fatalf("upload concluído: %+v", v)
	}
	a.exigirErro("ana", http.MethodGet, base+"/videos/"+v.ID.String()+"/download", nil, 409, "video_nao_pronto")
	if js := a.rodar("processar_video"); len(js) != 1 {
		t.Fatalf("processar_video enfileirados: %d", len(js))
	}
	prefixo := "videos/" + strings.TrimPrefix(base, "/v1/workspaces/") + "/" + v.ID.String() + "/"
	if got := a.bucket.chaves(prefixo); !slices.Equal(got, []string{prefixo + "original", prefixo + "preview.mp4", prefixo + "thumb.jpg"}) {
		t.Fatalf("objetos: %v", got)
	}

	var doProduto []midia.Video
	a.exigir("ana", http.MethodGet, base+"/videos?produto_id="+produto.String(), nil, &doProduto, 200)
	if !slices.Equal(ids(doProduto), []uuid.UUID{ref.ID, v.ID}) {
		t.Fatalf("vídeos do produto: %+v", doProduto)
	}
	pronto := doProduto[1]
	if pronto.Status != midia.StatusPronto || *pronto.DuracaoS != 12 || *pronto.Largura != 720 ||
		*pronto.PreviewURL != "https://s3.teste/"+prefixo+"preview.mp4" || *pronto.ThumbURL != "https://s3.teste/"+prefixo+"thumb.jpg" {
		t.Fatalf("upload pronto: %+v", pronto)
	}
	var dl struct{ URL string }
	a.exigir("ana", http.MethodGet, base+"/videos/"+v.ID.String()+"/download", nil, &dl, 200)
	if dl.URL != "https://s3.teste/"+prefixo+"original?baixar=Meu+unboxing_final.mp4" {
		t.Fatalf("download: %s", dl.URL)
	}
	var cota midia.Cota
	a.exigir("ana", http.MethodGet, base+"/videos/cota", nil, &cota, 200)
	if cota.UsadosBytes != 1000 || cota.LimiteBytes != 5<<30 {
		t.Fatalf("cota: %+v", cota)
	}

	// Desvincular do produto e ligar de novo; a biblioteca tem os três.
	a.exigir("ana", http.MethodDelete, base+"/videos/"+ref.ID.String()+"/produtos/"+produto.String(), nil, &ref, 200)
	if len(ref.ProdutoIDs) != 0 {
		t.Fatalf("desvincular: %+v", ref)
	}
	outro := a.produtoID(1)
	a.exigir("ana", http.MethodPut, base+"/videos/"+ref.ID.String()+"/produtos/"+outro.String(), nil, &ref, 200)
	a.exigir("ana", http.MethodGet, base+"/videos?produto_id="+produto.String(), nil, &doProduto, 200)
	if !slices.Equal(ids(doProduto), []uuid.UUID{v.ID}) {
		t.Fatalf("depois de desvincular: %v", ids(doProduto))
	}
	var todos []midia.Video
	a.exigir("ana", http.MethodGet, base+"/videos", nil, &todos, 200)
	if len(todos) != 3 {
		t.Fatalf("biblioteca: %d", len(todos))
	}

	// Renomear e apagar: o espaço volta para a cota e os arquivos somem.
	titulo := "  Unboxing   do fone "
	a.exigir("ana", http.MethodPatch, base+"/videos/"+v.ID.String(), map[string]any{"titulo": titulo}, &v, 200)
	if v.Titulo != "Unboxing do fone" {
		t.Fatalf("título: %q", v.Titulo)
	}
	a.exigir("ana", http.MethodDelete, base+"/videos/"+v.ID.String(), nil, nil, http.StatusNoContent)
	a.exigirErro("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, 404, "video_nao_encontrado")
	if got := a.bucket.chaves(prefixo); len(got) != 0 {
		t.Fatalf("objetos depois de apagar: %v", got)
	}
	a.exigir("ana", http.MethodGet, base+"/videos/cota", nil, &cota, 200)
	if cota.UsadosBytes != 0 {
		t.Fatalf("cota depois de apagar: %+v", cota)
	}
}

// TestRevalidarEmbed: o job semanal marca o vídeo que sumiu e agenda a
// próxima rodada.
func TestRevalidarEmbed(t *testing.T) {
	a := novoAmbiente(t)
	base := a.pessoal("ana")
	var v midia.Video
	a.exigir("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": youtubeOK2}, &v, 201)
	js := a.fila.tirar()
	if len(js) != 1 || js[0].Args.Kind() != "revalidar_embed" || time.Until(js[0].Em) < 6*24*time.Hour {
		t.Fatalf("agendados: %+v", js)
	}
	_ = a.fila.Enfileirar(context.Background(), js...)

	a.oembed.remover("https://www.youtube.com/watch?v=aaaaaaaaaaa")
	a.rodar("revalidar_embed")
	a.exigir("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, &v, 200)
	if v.Status != midia.StatusIndisponivel {
		t.Fatalf("status depois de sumir: %s", v.Status)
	}
	js = a.fila.tirar()
	if len(js) != 1 || js[0].Args.Kind() != "revalidar_embed" || time.Until(js[0].Em) < 6*24*time.Hour {
		t.Fatalf("próxima rodada: %+v", js)
	}

	// Apagado, a revalidação para.
	a.exigir("ana", http.MethodDelete, base+"/videos/"+v.ID.String(), nil, nil, http.StatusNoContent)
	_ = a.fila.Enfileirar(context.Background(), js...)
	a.rodar("revalidar_embed")
	if js := a.fila.tirar(); len(js) != 0 {
		t.Fatalf("revalidou vídeo apagado: %+v", js)
	}
}

// TestUploadsLimitesECota cobre as validações do upload, a cota do plano, o
// arquivo maior do que o informado e a limpeza de uploads abandonados.
func TestUploadsLimitesECota(t *testing.T) {
	a := novoAmbiente(t)
	base := a.pessoal("ana")
	ok := map[string]any{"nome": "a.mp4", "content_type": "video/mp4", "tamanho": 100, "direito_uso": true}
	com := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	a.exigirErro("ana", http.MethodPost, base+"/videos/uploads", com("direito_uso", false), 422, "direito_uso_obrigatorio")
	a.exigirErro("ana", http.MethodPost, base+"/videos/uploads", com("content_type", "image/png"), 422, "formato_invalido")
	a.exigirErro("ana", http.MethodPost, base+"/videos/uploads", com("tamanho", midia.MaxBytes+1), 422, "arquivo_grande")
	a.exigirErro("ana", http.MethodPost, base+"/videos/uploads", com("tamanho", 0), 422, "dados_invalidos")

	// Cota de 150 bytes no plano avulso.
	if _, err := a.pool.Exec(context.Background(), "UPDATE limites SET valor = 150 WHERE plano = 'avulso' AND chave = 'video_bytes'"); err != nil {
		t.Fatal(err)
	}
	var u1 midia.UploadIniciado
	a.exigir("ana", http.MethodPost, base+"/videos/uploads", ok, &u1, 201)
	// O upload em andamento reserva o espaço.
	a.exigirErro("ana", http.MethodPost, base+"/videos/uploads", ok, 409, "cota_videos")
	var cota midia.Cota
	a.exigir("ana", http.MethodGet, base+"/videos/cota", nil, &cota, 200)
	if cota.UsadosBytes != 100 || cota.LimiteBytes != 150 {
		t.Fatalf("cota com upload aberto: %+v", cota)
	}

	// Abandonado: o limpar_upload agendado para 24 h descarta tudo.
	js := a.fila.tirar()
	if len(js) != 1 || js[0].Args.Kind() != "limpar_upload" || time.Until(js[0].Em) < 23*time.Hour {
		t.Fatalf("agendados: %+v", js)
	}
	_ = a.fila.Enfileirar(context.Background(), js...)
	a.rodar("limpar_upload")
	a.exigirErro("ana", http.MethodGet, base+"/videos/"+u1.Video.ID.String(), nil, 404, "video_nao_encontrado")
	if a.bucket.abertos() != 0 {
		t.Fatal("o upload multipart ficou aberto")
	}
	a.exigir("ana", http.MethodGet, base+"/videos/cota", nil, &cota, 200)
	if cota.UsadosBytes != 0 {
		t.Fatalf("cota depois da limpeza: %+v", cota)
	}

	// Concluído menor do que o informado: a cota fica com o tamanho real, e
	// o limpar_upload não mexe nele.
	v := a.enviar("ana", base, "b.mp4", bytes.Repeat([]byte("x"), 60), nil)
	a.exigir("ana", http.MethodGet, base+"/videos/cota", nil, &cota, 200)
	if v.TamanhoBytes != 60 || cota.UsadosBytes != 60 {
		t.Fatalf("depois de concluir: %+v %+v", v, cota)
	}
	a.rodar("limpar_upload")
	a.exigir("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, &v, 200)

	// Maior do que o informado: descartado.
	var u2 midia.UploadIniciado
	a.exigir("ana", http.MethodPost, base+"/videos/uploads", com("tamanho", 10), &u2, 201)
	video := base + "/videos/" + u2.Video.ID.String()
	var assinada struct{ URL string }
	a.exigir("ana", http.MethodPost, video+"/partes", map[string]any{"numero": 1}, &assinada, 200)
	p := a.bucket.receber(t, assinada.URL, bytes.Repeat([]byte("y"), 50))
	var partes []map[string]any
	a.exigir("ana", http.MethodGet, video+"/partes", nil, &partes, 200)
	if len(partes) != 1 || partes[0]["tamanho"].(float64) != 50 {
		t.Fatalf("partes: %+v", partes)
	}
	a.exigirErro("ana", http.MethodPost, video+"/concluir", map[string]any{"partes": []map[string]any{{"numero": 1, "etag": "errado"}}}, 422, "upload_incompleto")
	a.exigirErro("ana", http.MethodPost, video+"/concluir", map[string]any{"partes": []map[string]any{{"numero": 1, "etag": p.ETag}}}, 422, "upload_incompleto")
	a.exigirErro("ana", http.MethodGet, video, nil, 404, "video_nao_encontrado")
	a.exigir("ana", http.MethodGet, base+"/videos/cota", nil, &cota, 200)
	if cota.UsadosBytes != 60 {
		t.Fatalf("cota depois do descarte: %+v", cota)
	}
	a.exigirErro("ana", http.MethodPost, video+"/partes", map[string]any{"numero": 2}, 404, "video_nao_encontrado")
	a.exigirErro("ana", http.MethodPost, base+"/videos/"+v.ID.String()+"/partes", map[string]any{"numero": 1}, 409, "upload_encerrado")
	a.exigirErro("ana", http.MethodPost, base+"/videos/"+v.ID.String()+"/partes", map[string]any{"numero": 0}, 422, "dados_invalidos")
}

// TestArquivoQueNaoEhVideo: o processar_video marca falhou e não tenta de novo.
func TestArquivoQueNaoEhVideo(t *testing.T) {
	a := novoAmbiente(t)
	a.worker = midia.NewService(a.pool, a.produtos, &midia.OEmbed{}, a.bucket, processador{recusar: true},
		contas.NewService(a.pool, nil, ""), a.fila, slog.New(slog.NewTextHandler(io.Discard, nil)))
	base := a.pessoal("ana")
	v := a.enviar("ana", base, "nota.mp4", []byte("texto qualquer"), nil)
	for _, j := range a.fila.tirar() {
		args, ok := j.Args.(midia.ProcessarVideoArgs)
		if !ok {
			continue
		}
		w := &midia.ProcessarVideoWorker{Svc: a.worker, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		err := w.Work(context.Background(), &river.Job[midia.ProcessarVideoArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3}, Args: args})
		var cancel *river.JobCancelError
		if !errors.As(err, &cancel) {
			t.Fatalf("processar_video: %v, quer JobCancel", err)
		}
	}
	a.exigir("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, &v, 200)
	if v.Status != midia.StatusFalhou {
		t.Fatalf("status: %s", v.Status)
	}
}

// TestCompartilharEListas: os vídeos do mentor chegam à turma quando ele os
// compartilha ou anexa a uma lista; o afiliado não compartilha nem altera o
// vídeo de outra pessoa.
func TestCompartilharEListas(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mestre", "ana")
	produto := a.produtoID(2)

	var ref midia.Video
	a.exigir("mestre", http.MethodPost, ws+"/videos/embed", map[string]any{"url": youtubeOK, "produto_id": produto}, &ref, 201)
	proprio := a.enviar("mestre", ws, "aula.mp4", bytes.Repeat([]byte("m"), 40), &produto)
	a.rodar("processar_video")

	// Antes de compartilhar, a turma não vê.
	var vs []midia.Video
	a.exigir("ana", http.MethodGet, ws+"/videos", nil, &vs, 200)
	if len(vs) != 0 {
		t.Fatalf("afiliada vê vídeos não compartilhados: %v", ids(vs))
	}
	a.exigirErro("ana", http.MethodGet, ws+"/videos/"+ref.ID.String(), nil, 404, "video_nao_encontrado")

	// O mentor compartilha o vídeo próprio: a afiliada vê no produto e baixa.
	a.exigir("mestre", http.MethodPatch, ws+"/videos/"+proprio.ID.String(), map[string]any{"compartilhado": true}, &proprio, 200)
	a.exigir("ana", http.MethodGet, ws+"/videos?produto_id="+produto.String(), nil, &vs, 200)
	if len(vs) != 1 || vs[0].ID != proprio.ID || vs[0].Meu || vs[0].PreviewURL == nil || len(vs[0].ProdutoIDs) != 0 {
		t.Fatalf("vídeos do produto para a afiliada: %+v", vs)
	}
	var dl struct{ URL string }
	a.exigir("ana", http.MethodGet, ws+"/videos/"+proprio.ID.String()+"/download", nil, &dl, 200)
	a.exigirErro("ana", http.MethodPatch, ws+"/videos/"+proprio.ID.String(), map[string]any{"titulo": "meu"}, 403, "sem_permissao")
	a.exigirErro("ana", http.MethodDelete, ws+"/videos/"+proprio.ID.String(), nil, 403, "sem_permissao")
	a.exigirErro("ana", http.MethodPut, ws+"/videos/"+proprio.ID.String()+"/produtos/"+a.produtoID(3).String(), nil, 403, "sem_permissao")

	// A afiliada não compartilha os dela (nem no workspace pessoal).
	var dela midia.Video
	a.exigir("ana", http.MethodPost, ws+"/videos/embed", map[string]any{"url": youtubeOK2}, &dela, 201)
	a.exigirErro("ana", http.MethodPatch, ws+"/videos/"+dela.ID.String(), map[string]any{"compartilhado": true}, 403, "sem_permissao")
	pessoal := a.pessoal("ana")
	var noPessoal midia.Video
	a.exigir("ana", http.MethodPost, pessoal+"/videos/embed", map[string]any{"url": youtubeOK2}, &noPessoal, 201)
	a.exigirErro("ana", http.MethodPatch, pessoal+"/videos/"+noPessoal.ID.String(), map[string]any{"compartilhado": true}, 403, "sem_permissao")
	// Nem pela RLS, mesmo sem passar pelo serviço.
	anaID := a.usuarioID("ana")
	err := database.InTx(context.Background(), a.pool, database.Scope{UserID: anaID.String(), WorkspaceID: strings.TrimPrefix(ws, "/v1/workspaces/")}, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE videos SET compartilhado = true WHERE id = $1", dela.ID)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("RLS deixou a afiliada compartilhar: %v", err)
	}

	// O mentor anexa a referência a uma lista: ela passa a ser compartilhada.
	var l curadoria.ListaDetalhe
	a.exigir("mestre", http.MethodPost, ws+"/listas", map[string]any{"titulo": "Achados"}, &l, 201)
	lista := ws + "/listas/" + l.ID.String()
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": produto, "comentario": "Veja os vídeos"}, &l, 201)
	a.exigir("mestre", http.MethodPut, lista+"/videos/"+ref.ID.String(), nil, &l, 200)
	if !slices.Equal(ids(l.Videos), []uuid.UUID{ref.ID}) || !l.Videos[0].Compartilhado ||
		!slices.Equal(ids(l.Itens[0].Videos), []uuid.UUID{ref.ID, proprio.ID}) {
		t.Fatalf("lista com vídeos: %+v / %+v", l.Videos, l.Itens[0].Videos)
	}
	a.exigirErro("ana", http.MethodPut, lista+"/videos/"+dela.ID.String(), nil, 403, "sem_permissao")
	a.exigirErro("mestre", http.MethodPut, lista+"/videos/"+dela.ID.String(), nil, 404, "video_nao_encontrado")
	a.exigirErro("mestre", http.MethodPut, ws+"/listas/"+uuid.NewString()+"/videos/"+ref.ID.String(), nil, 404, "lista_nao_encontrada")

	// Publicada, a afiliada vê os vídeos da lista e os do produto.
	a.exigir("mestre", http.MethodPost, lista+"/publicar", nil, &l, 200)
	a.exigir("ana", http.MethodGet, lista, nil, &l, 200)
	if !slices.Equal(ids(l.Videos), []uuid.UUID{ref.ID}) || !slices.Equal(ids(l.Itens[0].Videos), []uuid.UUID{ref.ID, proprio.ID}) {
		t.Fatalf("lista para a afiliada: %+v / %+v", l.Videos, l.Itens[0].Videos)
	}

	// Tirar da lista; apagar a lista leva os vínculos, não os vídeos.
	a.exigir("mestre", http.MethodDelete, lista+"/videos/"+ref.ID.String(), nil, &l, 200)
	if len(l.Videos) != 0 {
		t.Fatalf("vídeos depois de tirar: %+v", l.Videos)
	}
	a.exigirErro("mestre", http.MethodDelete, lista+"/videos/"+ref.ID.String(), nil, 404, "video_nao_encontrado")
	a.exigir("mestre", http.MethodPut, lista+"/videos/"+proprio.ID.String(), nil, &l, 200)
	a.exigir("mestre", http.MethodDelete, lista, nil, nil, http.StatusNoContent)
	a.exigir("mestre", http.MethodGet, ws+"/videos/"+proprio.ID.String(), nil, &proprio, 200)
	if len(proprio.ListaIDs) != 0 || len(proprio.ProdutoIDs) != 1 {
		t.Fatalf("vínculos depois de apagar a lista: %+v", proprio)
	}
}

func (a *ambiente) usuarioID(sub string) uuid.UUID {
	a.t.Helper()
	var u contas.Usuario
	a.exigir(sub, http.MethodGet, "/v1/eu", nil, &u, 200)
	return u.ID
}

// TestVazamento: vídeos e cota de um workspace não aparecem em outro, nem
// para o mesmo usuário.
func TestVazamento(t *testing.T) {
	a := novoAmbiente(t)
	wsA := a.mentoria("mestre", "ana")
	wsB := a.pessoal("beto")
	produto := a.produtoID(0)

	var ref midia.Video
	a.exigir("mestre", http.MethodPost, wsA+"/videos/embed", map[string]any{"url": youtubeOK, "produto_id": produto}, &ref, 201)
	v := a.enviar("mestre", wsA, "a.mp4", bytes.Repeat([]byte("a"), 20), &produto)
	a.rodar("processar_video")
	a.exigir("mestre", http.MethodPatch, wsA+"/videos/"+v.ID.String(), map[string]any{"compartilhado": true}, &v, 200)

	for _, alheio := range []string{wsB + "/videos/" + ref.ID.String(), wsB + "/videos/" + v.ID.String(), wsA + "/videos/" + v.ID.String()} {
		sub := "beto"
		if strings.HasPrefix(alheio, wsA) {
			// beto não é membro do workspace A.
			a.exigirErro(sub, http.MethodGet, alheio, nil, 404, "workspace_nao_encontrado")
			continue
		}
		a.exigirErro(sub, http.MethodGet, alheio, nil, 404, "video_nao_encontrado")
		a.exigirErro(sub, http.MethodPatch, alheio, map[string]any{"titulo": "x"}, 404, "video_nao_encontrado")
		a.exigirErro(sub, http.MethodDelete, alheio, nil, 404, "video_nao_encontrado")
		a.exigirErro(sub, http.MethodGet, alheio+"/download", nil, 404, "video_nao_encontrado")
	}
	var vs []midia.Video
	a.exigir("beto", http.MethodGet, wsB+"/videos?produto_id="+produto.String(), nil, &vs, 200)
	a.exigir("beto", http.MethodGet, wsB+"/videos", nil, &vs, 200)
	if len(vs) != 0 {
		t.Fatalf("beto vê vídeos de outro workspace: %v", ids(vs))
	}
	// O mestre, no workspace pessoal dele, também não vê os da mentoria.
	pessoalMestre := a.pessoal("mestre")
	a.exigir("mestre", http.MethodGet, pessoalMestre+"/videos?produto_id="+produto.String(), nil, &vs, 200)
	if len(vs) != 0 {
		t.Fatalf("vídeos da mentoria no workspace pessoal: %v", ids(vs))
	}
	a.exigirErro("mestre", http.MethodGet, pessoalMestre+"/videos/"+v.ID.String(), nil, 404, "video_nao_encontrado")
	var cota midia.Cota
	a.exigir("mestre", http.MethodGet, pessoalMestre+"/videos/cota", nil, &cota, 200)
	if cota.UsadosBytes != 0 {
		t.Fatalf("cota do pessoal com uso da mentoria: %+v", cota)
	}

	// Direto no banco, com o escopo do workspace B: nada de A.
	betoID := a.usuarioID("beto")
	err := database.InTx(context.Background(), a.pool, database.Scope{UserID: betoID.String(), WorkspaceID: strings.TrimPrefix(wsB, "/v1/workspaces/")}, func(tx pgx.Tx) error {
		for _, q := range []string{"SELECT count(*) FROM videos", "SELECT count(*) FROM video_vinculos", "SELECT count(*) FROM uso_videos"} {
			var n int
			if err := tx.QueryRow(context.Background(), q).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				return fmt.Errorf("%s = %d", q, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestFilaRiver enfileira os jobs de mídia no River de verdade, que valida as
// opções de cada um: o processamento não entra duas vezes, e a revalidação
// pode agendar a próxima rodada de si mesma.
func TestFilaRiver(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := queue.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := queue.NewInsertClient(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	fila := &midia.FilaRiver{Client: client}
	ws, dono, id := uuid.New(), uuid.New(), uuid.New()
	depois := time.Now().Add(time.Hour)
	if err := fila.Enfileirar(ctx,
		midia.Job{Args: midia.ProcessarVideoArgs{VideoID: id, WorkspaceID: ws, DonoID: dono}},
		midia.Job{Args: midia.LimparUploadArgs{VideoID: id, WorkspaceID: ws, DonoID: dono}, Em: depois},
		midia.Job{Args: midia.RevalidarEmbedArgs{VideoID: id, WorkspaceID: ws, DonoID: dono}, Em: depois},
	); err != nil {
		t.Fatal(err)
	}
	if err := fila.Enfileirar(ctx,
		midia.Job{Args: midia.ProcessarVideoArgs{VideoID: id, WorkspaceID: ws, DonoID: dono}},
		midia.Job{Args: midia.RevalidarEmbedArgs{VideoID: id, WorkspaceID: ws, DonoID: dono}, Em: depois.Add(time.Hour)},
	); err != nil {
		t.Fatal(err)
	}
	contar := func(kind, estado string) (n int) {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = $1 AND state::text = $2", kind, estado).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, c := range []struct {
		kind, estado string
		quer         int
	}{
		{"processar_video", "available", 1},
		{"limpar_upload", "scheduled", 1},
		{"revalidar_embed", "scheduled", 2},
	} {
		if n := contar(c.kind, c.estado); n != c.quer {
			t.Errorf("%s %s: %d, quer %d", c.kind, c.estado, n, c.quer)
		}
	}
}
