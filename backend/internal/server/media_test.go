package server

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
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/oembed"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
)

const (
	mediaYouTubeOK   = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	mediaYouTubeOK2  = "https://youtu.be/aaaaaaaaaaa"
	mediaTikTokShort = "https://vm.tiktok.com/ZMabc123/"
	mediaTikTokID    = "7350000000000000001"
)

// mediaBucket is an in-memory S3 with multipart uploads. The parts arrive by
// receive, in place of the browser's PUT.
type mediaBucket struct {
	mu      sync.Mutex
	dir     string
	objects map[string][]byte
	uploads map[string]*mediaBucketUpload
	seq     int
}

type mediaBucketUpload struct {
	key   string
	parts map[int][]byte
}

func newMediaBucket(t *testing.T) *mediaBucket {
	return &mediaBucket{dir: t.TempDir(), objects: map[string][]byte{}, uploads: map[string]*mediaBucketUpload{}}
}

func mediaETag(data []byte) string { return fmt.Sprintf(`"%x"`, len(data)*31+int(data[0])) }

var errMediaBucketNotFound = fmt.Errorf("fake bucket: %w", domain.ErrNotFound)

func (b *mediaBucket) StartMultipart(_ context.Context, key, _ string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := fmt.Sprintf("up-%d", b.seq)
	b.uploads[id] = &mediaBucketUpload{key: key, parts: map[int][]byte{}}
	return id, nil
}

func (b *mediaBucket) SignPart(_ context.Context, key, uploadID string, number int, _ time.Duration) (string, error) {
	return fmt.Sprintf("https://s3.test/%s?partNumber=%d&uploadId=%s", key, number, uploadID), nil
}

func (b *mediaBucket) receive(t *testing.T, partURL string, data []byte) domain.UploadPart {
	t.Helper()
	u, err := url.Parse(partURL)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	up, ok := b.uploads[u.Query().Get("uploadId")]
	if !ok || "/"+up.key != u.Path {
		t.Fatalf("part for an unknown upload: %s", partURL)
	}
	var n int
	_, _ = fmt.Sscan(u.Query().Get("partNumber"), &n)
	up.parts[n] = data
	return domain.UploadPart{Number: n, ETag: mediaETag(data)}
}

func (b *mediaBucket) Parts(_ context.Context, _, uploadID string) ([]domain.UploadPart, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	up, ok := b.uploads[uploadID]
	if !ok {
		return nil, errMediaBucketNotFound
	}
	var out []domain.UploadPart
	for n, d := range up.parts {
		out = append(out, domain.UploadPart{Number: n, ETag: mediaETag(d), Size: int64(len(d))})
	}
	slices.SortFunc(out, func(a, b domain.UploadPart) int { return a.Number - b.Number })
	return out, nil
}

func (b *mediaBucket) CompleteMultipart(_ context.Context, key, uploadID string, parts []domain.UploadPart) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	up, ok := b.uploads[uploadID]
	if !ok {
		return errMediaBucketNotFound
	}
	var all []byte
	for _, p := range parts {
		d, ok := up.parts[p.Number]
		if !ok || mediaETag(d) != p.ETag {
			return fmt.Errorf("InvalidPart %d", p.Number)
		}
		all = append(all, d...)
	}
	b.objects[key] = all
	delete(b.uploads, uploadID)
	return nil
}

func (b *mediaBucket) AbortMultipart(_ context.Context, _, uploadID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.uploads, uploadID)
	return nil
}

func (b *mediaBucket) Info(_ context.Context, key string) (domain.ObjectInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.objects[key]
	if !ok {
		return domain.ObjectInfo{}, errMediaBucketNotFound
	}
	return domain.ObjectInfo{Size: int64(len(d))}, nil
}

func (b *mediaBucket) SignGet(_ context.Context, key string, _ time.Duration, downloadAs string) (string, error) {
	u := "https://s3.test/" + key
	if downloadAs != "" {
		u += "?download=" + url.QueryEscape(downloadAs)
	}
	return u, nil
}

func (b *mediaBucket) InternalURL(_ context.Context, key string, _ time.Duration) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := filepath.Join(b.dir, strings.ReplaceAll(key, "/", "_"))
	return f, os.WriteFile(f, b.objects[key], 0o600)
}

func (b *mediaBucket) UploadFile(_ context.Context, key, path, _ string) error {
	d, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = d
	return nil
}

func (b *mediaBucket) DeletePrefix(_ context.Context, prefix string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.objects {
		if strings.HasPrefix(k, prefix) {
			delete(b.objects, k)
		}
	}
	return nil
}

func (b *mediaBucket) keys(prefix string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for k := range b.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (b *mediaBucket) open() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.uploads)
}

// mediaProcessor plays ffmpeg: it writes preview and thumbnail in the dir.
type mediaProcessor struct{ refuse bool }

func (p mediaProcessor) Process(_ context.Context, input, dir string) (domain.ProcessedVideo, error) {
	if _, err := os.Stat(input); err != nil {
		return domain.ProcessedVideo{}, err
	}
	if p.refuse {
		return domain.ProcessedVideo{}, domain.ErrNotAVideo
	}
	out := domain.ProcessedVideo{DurationS: 12, Width: 720, Height: 1280,
		PreviewPath: filepath.Join(dir, "p.mp4"), ThumbnailPath: filepath.Join(dir, "t.jpg")}
	if err := os.WriteFile(out.PreviewPath, []byte("preview"), 0o600); err != nil {
		return out, err
	}
	return out, os.WriteFile(out.ThumbnailPath, []byte("thumb"), 0o600)
}

// mediaOEmbed answers like the official YouTube and TikTok endpoints.
type mediaOEmbed struct {
	mu      sync.Mutex
	removed map[string]bool
}

func (o *mediaOEmbed) remove(link string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.removed[link] = true
}

func (o *mediaOEmbed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	link := r.URL.Query().Get("url")
	o.mu.Lock()
	removed := o.removed[link]
	o.mu.Unlock()
	if removed {
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
			"thumbnail_url": "https://p16-sign.tiktokcdn.com/thumb.jpeg", "embed_product_id": mediaTikTokID,
		})
	default:
		http.NotFound(w, r)
	}
}

// queuedVideoJob is a media job the fake queue received.
type queuedVideoJob struct {
	kind string
	job  domain.VideoJob
	at   time.Time
}

type fakeMediaQueue struct {
	mu   sync.Mutex
	jobs []queuedVideoJob
}

func (q *fakeMediaQueue) add(kind string, j domain.VideoJob, at time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, queuedVideoJob{kind: kind, job: j, at: at})
	return nil
}

func (q *fakeMediaQueue) EnqueueProcessVideo(_ context.Context, j domain.VideoJob) error {
	return q.add("process_video", j, time.Time{})
}

func (q *fakeMediaQueue) ScheduleRevalidateEmbed(_ context.Context, j domain.VideoJob, at time.Time) error {
	return q.add("revalidate_embed", j, at)
}

func (q *fakeMediaQueue) ScheduleCleanUpload(_ context.Context, j domain.VideoJob, at time.Time) error {
	return q.add("clean_upload", j, at)
}

func (q *fakeMediaQueue) take() []queuedVideoJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.jobs
	q.jobs = nil
	return out
}

func (q *fakeMediaQueue) putBack(js []queuedVideoJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, js...)
}

// mediaEnv is the test app with the media fakes.
type mediaEnv struct {
	*testApp
	bucket   *mediaBucket
	oembed   *mediaOEmbed
	queue    *fakeMediaQueue
	products []uuid.UUID
}

func newMediaEnv(t *testing.T, processor domain.VideoProcessor) *mediaEnv {
	t.Helper()
	oe := &mediaOEmbed{removed: map[string]bool{}}
	srv := httptest.NewServer(oe)
	t.Cleanup(srv.Close)
	e := &mediaEnv{bucket: newMediaBucket(t), oembed: oe, queue: &fakeMediaQueue{}}
	e.testApp = newTestApp(t, func(in *infra) {
		in.objects = e.bucket
		in.embeds = &oembed.Client{HTTP: srv.Client(), YouTubeURL: srv.URL + "/youtube", TikTokURL: srv.URL + "/tiktok"}
		in.videoProcessor = processor
		in.mediaQueue = e.queue
	})
	ctx := context.Background()
	for i := range 5 {
		p, err := e.svcs.products.Import(ctx, domain.SourceShopee, domain.Offer{
			ItemID: int64(1000 + i), ShopID: 1, ShopName: "Loja", Name: fmt.Sprintf("Produto %d", i),
			URL: fmt.Sprintf("https://shopee.com.br/p-i.1.%d", 1000+i), MinPriceCents: 1000, MaxPriceCents: 1000,
			CommissionBP: 1000, Sales: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		e.products = append(e.products, p.ID)
	}
	return e
}

func mediaWorkerLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// run works the queued jobs (or only those of a kind) with the River workers.
func (e *mediaEnv) run(kind string) []queuedVideoJob {
	e.t.Helper()
	var ran, rest []queuedVideoJob
	ctx := context.Background()
	for _, j := range e.queue.take() {
		if kind != "" && j.kind != kind {
			rest = append(rest, j)
			continue
		}
		ran = append(ran, j)
		row := &rivertype.JobRow{Kind: j.kind, Attempt: 1, MaxAttempts: 3}
		args := queue.VideoJobArgs{VideoID: j.job.VideoID, WorkspaceID: j.job.WorkspaceID, OwnerID: j.job.OwnerID}
		var err error
		switch j.kind {
		case "process_video":
			err = (&queue.ProcessVideoWorker{Svc: e.svcs.media, Log: mediaWorkerLog()}).Work(ctx,
				&river.Job[queue.ProcessVideoArgs]{JobRow: row, Args: queue.ProcessVideoArgs{VideoJobArgs: args}})
		case "revalidate_embed":
			err = (&queue.RevalidateEmbedWorker{Svc: e.svcs.media}).Work(ctx,
				&river.Job[queue.RevalidateEmbedArgs]{JobRow: row, Args: queue.RevalidateEmbedArgs{VideoJobArgs: args}})
		case "clean_upload":
			err = (&queue.CleanUploadWorker{Svc: e.svcs.media}).Work(ctx,
				&river.Job[queue.CleanUploadArgs]{JobRow: row, Args: queue.CleanUploadArgs{VideoJobArgs: args}})
		}
		if err != nil {
			e.t.Fatalf("%s: %v", j.kind, err)
		}
	}
	e.queue.putBack(rest)
	return ran
}

// JSON of the media routes, as clients see it.

type videoJSON struct {
	ID           uuid.UUID   `json:"id"`
	Kind         string      `json:"kind"`
	Platform     string      `json:"platform"`
	Status       string      `json:"status"`
	Title        string      `json:"title"`
	Author       string      `json:"author"`
	URL          *string     `json:"url"`
	EmbedID      *string     `json:"embed_id"`
	PlayerURL    *string     `json:"player_url"`
	ThumbnailURL *string     `json:"thumbnail_url"`
	PreviewURL   *string     `json:"preview_url"`
	DurationS    *int32      `json:"duration_s"`
	Width        *int32      `json:"width"`
	Height       *int32      `json:"height"`
	SizeBytes    int64       `json:"size_bytes"`
	FileName     *string     `json:"file_name"`
	ContentType  *string     `json:"content_type"`
	Shared       bool        `json:"shared"`
	Mine         bool        `json:"mine"`
	ProductIDs   []uuid.UUID `json:"product_ids"`
	ListIDs      []uuid.UUID `json:"list_ids"`
	CreatedAt    time.Time   `json:"created_at"`
}

type startedUploadJSON struct {
	Video    videoJSON `json:"video"`
	UploadID string    `json:"upload_id"`
	Key      string    `json:"key"`
}

type videoQuotaJSON struct {
	UsedBytes  int64 `json:"used_bytes"`
	LimitBytes int64 `json:"limit_bytes"`
}

type signedURLJSON struct {
	URL string `json:"url"`
}

// upload walks the path of Uppy: start, send the parts through the signed
// URLs and complete. It returns the video in processing.
func (e *mediaEnv) upload(sub, base, name string, data []byte, productID *uuid.UUID) videoJSON {
	e.t.Helper()
	var u startedUploadJSON
	body := map[string]any{"file_name": name, "content_type": "video/mp4", "size_bytes": len(data), "usage_rights": true}
	if productID != nil {
		body["product_id"] = productID
	}
	e.must(sub, http.MethodPost, base+"/videos/uploads", body, &u, http.StatusCreated)
	video := base + "/videos/" + u.Video.ID.String()
	var parts []domain.UploadPart
	for i, chunk := range [][]byte{data[:len(data)/2], data[len(data)/2:]} {
		var signed signedURLJSON
		e.must(sub, http.MethodPost, video+"/parts", map[string]any{"number": i + 1}, &signed, http.StatusOK)
		parts = append(parts, e.bucket.receive(e.t, signed.URL, chunk))
	}
	var v videoJSON
	e.must(sub, http.MethodPost, video+"/complete", map[string]any{"parts": []map[string]any{
		{"number": parts[0].Number, "etag": parts[0].ETag}, {"number": parts[1].Number, "etag": parts[1].ETag},
	}}, &v, http.StatusOK)
	return v
}

func videoIDs(vs []videoJSON) []uuid.UUID {
	out := make([]uuid.UUID, len(vs))
	for i, v := range vs {
		out[i] = v.ID
	}
	return out
}

func (e *mediaEnv) member(sub string, ws uuid.UUID) domain.Member {
	e.t.Helper()
	m, err := e.svcs.accounts.Member(context.Background(), e.me(sub).ID, ws)
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

// TestVideosShowOnProduct is the criterion of M5: a reference video (embed)
// and an own video (upload) show on the product.
func TestVideosShowOnProduct(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	ws := e.personal("ana").ID
	base := wsPath(ws, "")
	product := e.products[0]

	// Reference: only oEmbed metadata and the official player.
	var ref videoJSON
	e.must("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": mediaYouTubeOK + "&t=42", "product_id": product}, &ref, http.StatusCreated)
	if ref.Kind != "embed" || ref.Platform != "youtube" || ref.Status != "ready" ||
		ref.Title != "Review do dQw4w9WgXcQ" || ref.Author != "Canal Achados" || *ref.URL != mediaYouTubeOK ||
		*ref.EmbedID != "dQw4w9WgXcQ" || *ref.PlayerURL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" ||
		ref.ThumbnailURL == nil || !ref.Mine || !slices.Equal(ref.ProductIDs, []uuid.UUID{product}) {
		t.Fatalf("reference: %+v", ref)
	}
	// Pasting again returns the same video.
	var again videoJSON
	e.must("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ"}, &again, http.StatusOK)
	if again.ID != ref.ID {
		t.Fatalf("pasting again created another video: %s", again.ID)
	}
	// TikTok short link: the id and the full URL come from the oEmbed.
	var tk videoJSON
	e.must("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": mediaTikTokShort}, &tk, http.StatusCreated)
	if *tk.URL != "https://www.tiktok.com/@criadora/video/"+mediaTikTokID || *tk.PlayerURL != "https://www.tiktok.com/player/v1/"+mediaTikTokID {
		t.Fatalf("tiktok: %+v", tk)
	}
	e.mustFail("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": "https://vimeo.com/123"}, 422, "invalid_video_link")
	e.oembed.remove("https://www.youtube.com/watch?v=bbbbbbbbbbb")
	e.mustFail("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": "https://youtu.be/bbbbbbbbbbb"}, 422, "video_unavailable")
	e.mustFail("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": mediaYouTubeOK, "product_id": uuid.New()}, 404, "product_not_found")

	// Own upload: the parts go straight to the bucket, the worker processes.
	data := bytes.Repeat([]byte("v"), 1000)
	v := e.upload("ana", base, "Meu unboxing_final.mp4", data, &product)
	if v.Status != "processing" || v.SizeBytes != 1000 || v.Title != "Meu unboxing final" || v.PreviewURL != nil ||
		*v.FileName != "Meu unboxing_final.mp4" || *v.ContentType != "video/mp4" {
		t.Fatalf("completed upload: %+v", v)
	}
	e.mustFail("ana", http.MethodGet, base+"/videos/"+v.ID.String()+"/download", nil, 409, "video_not_ready")
	if js := e.run("process_video"); len(js) != 1 {
		t.Fatalf("process_video queued: %d", len(js))
	}
	prefix := "videos/" + ws.String() + "/" + v.ID.String() + "/"
	if got := e.bucket.keys(prefix); !slices.Equal(got, []string{prefix + "original", prefix + "preview.mp4", prefix + "thumb.jpg"}) {
		t.Fatalf("objects: %v", got)
	}

	var ofProduct []videoJSON
	e.must("ana", http.MethodGet, base+"/videos?product_id="+product.String(), nil, &ofProduct, http.StatusOK)
	if !slices.Equal(videoIDs(ofProduct), []uuid.UUID{ref.ID, v.ID}) {
		t.Fatalf("videos of the product: %+v", ofProduct)
	}
	ready := ofProduct[1]
	if ready.Status != "ready" || *ready.DurationS != 12 || *ready.Width != 720 || *ready.Height != 1280 ||
		*ready.PreviewURL != "https://s3.test/"+prefix+"preview.mp4" || *ready.ThumbnailURL != "https://s3.test/"+prefix+"thumb.jpg" {
		t.Fatalf("ready upload: %+v", ready)
	}
	var dl signedURLJSON
	e.must("ana", http.MethodGet, base+"/videos/"+v.ID.String()+"/download", nil, &dl, http.StatusOK)
	if dl.URL != "https://s3.test/"+prefix+"original?download=Meu+unboxing_final.mp4" {
		t.Fatalf("download: %s", dl.URL)
	}
	e.mustFail("ana", http.MethodGet, base+"/videos/"+ref.ID.String()+"/download", nil, 422, "embed_not_downloadable")
	var quota videoQuotaJSON
	e.must("ana", http.MethodGet, base+"/videos/quota", nil, &quota, http.StatusOK)
	if quota.UsedBytes != 1000 || quota.LimitBytes != 5<<30 {
		t.Fatalf("quota: %+v", quota)
	}

	// Unlink from the product and link to another; the library has the three.
	e.must("ana", http.MethodDelete, base+"/videos/"+ref.ID.String()+"/products/"+product.String(), nil, &ref, http.StatusOK)
	if len(ref.ProductIDs) != 0 {
		t.Fatalf("unlink: %+v", ref)
	}
	other := e.products[1]
	e.must("ana", http.MethodPut, base+"/videos/"+ref.ID.String()+"/products/"+other.String(), nil, &ref, http.StatusOK)
	if !slices.Equal(ref.ProductIDs, []uuid.UUID{other}) {
		t.Fatalf("link: %+v", ref)
	}
	e.mustFail("ana", http.MethodPut, base+"/videos/"+ref.ID.String()+"/products/"+uuid.NewString(), nil, 404, "product_not_found")
	e.must("ana", http.MethodGet, base+"/videos?product_id="+product.String(), nil, &ofProduct, http.StatusOK)
	if !slices.Equal(videoIDs(ofProduct), []uuid.UUID{v.ID}) {
		t.Fatalf("after unlinking: %v", videoIDs(ofProduct))
	}
	var all []videoJSON
	e.must("ana", http.MethodGet, base+"/videos", nil, &all, http.StatusOK)
	if len(all) != 3 {
		t.Fatalf("library: %d", len(all))
	}
	e.must("ana", http.MethodGet, base+"/videos?mine=true", nil, &all, http.StatusOK)
	if len(all) != 3 {
		t.Fatalf("mine: %d", len(all))
	}

	// Rename and delete: the space goes back to the quota and the files go away.
	e.must("ana", http.MethodPatch, base+"/videos/"+v.ID.String(), map[string]any{"title": "  Unboxing   do fone "}, &v, http.StatusOK)
	if v.Title != "Unboxing do fone" {
		t.Fatalf("title: %q", v.Title)
	}
	e.mustFail("ana", http.MethodPatch, base+"/videos/"+v.ID.String(), map[string]any{"title": strings.Repeat("a", 201)}, 422, "video_title_too_long")
	e.must("ana", http.MethodDelete, base+"/videos/"+v.ID.String(), nil, nil, http.StatusNoContent)
	e.mustFail("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, 404, "video_not_found")
	e.mustFail("ana", http.MethodGet, base+"/videos/not-a-uuid", nil, 404, "video_not_found")
	if got := e.bucket.keys(prefix); len(got) != 0 {
		t.Fatalf("objects after deleting: %v", got)
	}
	e.must("ana", http.MethodGet, base+"/videos/quota", nil, &quota, http.StatusOK)
	if quota.UsedBytes != 0 {
		t.Fatalf("quota after deleting: %+v", quota)
	}
}

// TestRevalidateEmbed: the weekly job marks the video that is gone and
// schedules its next run.
func TestRevalidateEmbed(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	base := wsPath(e.personal("ana").ID, "")
	var v videoJSON
	e.must("ana", http.MethodPost, base+"/videos/embed", map[string]any{"url": mediaYouTubeOK2}, &v, http.StatusCreated)
	js := e.queue.take()
	if len(js) != 1 || js[0].kind != "revalidate_embed" || time.Until(js[0].at) < 6*24*time.Hour {
		t.Fatalf("scheduled: %+v", js)
	}
	e.queue.putBack(js)

	e.oembed.remove("https://www.youtube.com/watch?v=aaaaaaaaaaa")
	e.run("revalidate_embed")
	e.must("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, &v, http.StatusOK)
	if v.Status != "unavailable" {
		t.Fatalf("status after vanishing: %s", v.Status)
	}
	js = e.queue.take()
	if len(js) != 1 || js[0].kind != "revalidate_embed" || time.Until(js[0].at) < 6*24*time.Hour {
		t.Fatalf("next run: %+v", js)
	}

	// Deleted, the revalidation stops.
	e.must("ana", http.MethodDelete, base+"/videos/"+v.ID.String(), nil, nil, http.StatusNoContent)
	e.queue.putBack(js)
	e.run("revalidate_embed")
	if js := e.queue.take(); len(js) != 0 {
		t.Fatalf("revalidated a deleted video: %+v", js)
	}
}

// TestUploadLimitsAndQuota covers the upload checks, the plan quota, the file
// larger than reported and the cleanup of abandoned uploads.
func TestUploadLimitsAndQuota(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	base := wsPath(e.personal("ana").ID, "")
	ok := map[string]any{"file_name": "a.mp4", "content_type": "video/mp4", "size_bytes": 100, "usage_rights": true}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", with("usage_rights", false), 422, "usage_rights_required")
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", with("content_type", "image/png"), 422, "invalid_video_format")
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", with("size_bytes", domain.MaxVideoBytes+1), 422, "video_too_large")
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", with("size_bytes", 0), 422, "invalid_video_size")
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", with("product_id", uuid.New()), 404, "product_not_found")

	// Quota of 150 bytes in the solo plan.
	e.admin("UPDATE plan_limits SET value = 150 WHERE plan = 'solo' AND key = 'video_bytes'")
	var u1 startedUploadJSON
	e.must("ana", http.MethodPost, base+"/videos/uploads", ok, &u1, http.StatusCreated)
	if u1.Video.Status != "uploading" || u1.UploadID == "" || !strings.HasSuffix(u1.Key, "/"+u1.Video.ID.String()+"/original") {
		t.Fatalf("started upload: %+v", u1)
	}
	// The open upload reserves the space.
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", ok, 409, "video_quota_exceeded")
	e.mustFail("ana", http.MethodPost, base+"/videos/uploads", with("size_bytes", 151), 409, "video_quota_exceeded")
	var quota videoQuotaJSON
	e.must("ana", http.MethodGet, base+"/videos/quota", nil, &quota, http.StatusOK)
	if quota.UsedBytes != 100 || quota.LimitBytes != 150 {
		t.Fatalf("quota with an open upload: %+v", quota)
	}
	if e.bucket.open() != 1 {
		t.Fatalf("refused uploads left multipart uploads open: %d", e.bucket.open())
	}

	// Abandoned: the clean_upload scheduled for 24 h discards everything.
	js := e.queue.take()
	if len(js) != 1 || js[0].kind != "clean_upload" || time.Until(js[0].at) < 23*time.Hour {
		t.Fatalf("scheduled: %+v", js)
	}
	e.queue.putBack(js)
	e.run("clean_upload")
	e.mustFail("ana", http.MethodGet, base+"/videos/"+u1.Video.ID.String(), nil, 404, "video_not_found")
	if e.bucket.open() != 0 {
		t.Fatal("the multipart upload stayed open")
	}
	e.must("ana", http.MethodGet, base+"/videos/quota", nil, &quota, http.StatusOK)
	if quota.UsedBytes != 0 {
		t.Fatalf("quota after the cleanup: %+v", quota)
	}

	// Completed smaller than reported: the quota keeps the real size, and
	// clean_upload leaves it alone.
	v := e.upload("ana", base, "b.mp4", bytes.Repeat([]byte("x"), 60), nil)
	e.must("ana", http.MethodGet, base+"/videos/quota", nil, &quota, http.StatusOK)
	if v.SizeBytes != 60 || quota.UsedBytes != 60 {
		t.Fatalf("after completing: %+v %+v", v, quota)
	}
	e.run("clean_upload")
	e.must("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, &v, http.StatusOK)

	// Larger than reported: discarded.
	var u2 startedUploadJSON
	e.must("ana", http.MethodPost, base+"/videos/uploads", with("size_bytes", 10), &u2, http.StatusCreated)
	video := base + "/videos/" + u2.Video.ID.String()
	var signed signedURLJSON
	e.must("ana", http.MethodPost, video+"/parts", map[string]any{"number": 1}, &signed, http.StatusOK)
	p := e.bucket.receive(t, signed.URL, bytes.Repeat([]byte("y"), 50))
	var parts []map[string]any
	e.must("ana", http.MethodGet, video+"/parts", nil, &parts, http.StatusOK)
	if len(parts) != 1 || parts[0]["number"].(float64) != 1 || parts[0]["size"].(float64) != 50 || parts[0]["etag"] != p.ETag {
		t.Fatalf("parts: %+v", parts)
	}
	e.mustFail("ana", http.MethodPost, video+"/complete", map[string]any{"parts": []map[string]any{}}, 422, "upload_parts_required")
	e.mustFail("ana", http.MethodPost, video+"/complete", map[string]any{"parts": []map[string]any{{"number": 1, "etag": "wrong"}}}, 422, "upload_incomplete")
	e.mustFail("ana", http.MethodPost, video+"/complete", map[string]any{"parts": []map[string]any{{"number": 1, "etag": p.ETag}}}, 422, "upload_incomplete")
	e.mustFail("ana", http.MethodGet, video, nil, 404, "video_not_found")
	e.must("ana", http.MethodGet, base+"/videos/quota", nil, &quota, http.StatusOK)
	if quota.UsedBytes != 60 {
		t.Fatalf("quota after the discard: %+v", quota)
	}
	e.mustFail("ana", http.MethodPost, video+"/parts", map[string]any{"number": 2}, 404, "video_not_found")
	e.mustFail("ana", http.MethodPost, base+"/videos/"+v.ID.String()+"/parts", map[string]any{"number": 1}, 409, "upload_closed")
	e.mustFail("ana", http.MethodGet, base+"/videos/"+v.ID.String()+"/parts", nil, 409, "upload_closed")
	e.mustFail("ana", http.MethodPost, base+"/videos/"+v.ID.String()+"/parts", map[string]any{"number": 0}, 422, "invalid_part_number")
	e.mustFail("ana", http.MethodPost, base+"/videos/"+v.ID.String()+"/parts", map[string]any{"number": domain.MaxVideoParts + 1}, 422, "invalid_part_number")
}

// TestUploadsWithoutBucket: without a bucket only embeds work.
func TestUploadsWithoutBucket(t *testing.T) {
	a := newTestApp(t)
	base := wsPath(a.personal("ana").ID, "")
	a.mustFail("ana", http.MethodPost, base+"/videos/uploads",
		map[string]any{"file_name": "a.mp4", "content_type": "video/mp4", "size_bytes": 100, "usage_rights": true}, 503, "uploads_unavailable")
}

// TestFileThatIsNotAVideo: process_video marks it failed and does not retry.
func TestFileThatIsNotAVideo(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{refuse: true})
	base := wsPath(e.personal("ana").ID, "")
	v := e.upload("ana", base, "note.mp4", []byte("any text"), nil)
	for _, j := range e.queue.take() {
		if j.kind != "process_video" {
			continue
		}
		w := &queue.ProcessVideoWorker{Svc: e.svcs.media, Log: mediaWorkerLog()}
		args := queue.ProcessVideoArgs{VideoJobArgs: queue.VideoJobArgs{VideoID: j.job.VideoID, WorkspaceID: j.job.WorkspaceID, OwnerID: j.job.OwnerID}}
		err := w.Work(context.Background(), &river.Job[queue.ProcessVideoArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3}, Args: args})
		var cancel *river.JobCancelError
		if !errors.As(err, &cancel) {
			t.Fatalf("process_video: %v, want JobCancel", err)
		}
	}
	e.must("ana", http.MethodGet, base+"/videos/"+v.ID.String(), nil, &v, http.StatusOK)
	if v.Status != "failed" {
		t.Fatalf("status: %s", v.Status)
	}
	var dl signedURLJSON
	e.must("ana", http.MethodGet, base+"/videos/"+v.ID.String()+"/download", nil, &dl, http.StatusOK)
}

// TestShareAndLists: the mentor's videos reach the group when shared or
// attached to a list; the affiliate neither shares nor changes someone
// else's video. The curation attaches through MediaService.
func TestShareAndLists(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	ctx := context.Background()
	wsID := e.mentorship("mentor", "Turma").ID
	e.join("mentor", "ana", wsID)
	ws := wsPath(wsID, "")
	product := e.products[2]

	var ref videoJSON
	e.must("mentor", http.MethodPost, ws+"/videos/embed", map[string]any{"url": mediaYouTubeOK, "product_id": product}, &ref, http.StatusCreated)
	own := e.upload("mentor", ws, "aula.mp4", bytes.Repeat([]byte("m"), 40), &product)
	e.run("process_video")

	// Before sharing, the group does not see them.
	var vs []videoJSON
	e.must("ana", http.MethodGet, ws+"/videos", nil, &vs, http.StatusOK)
	if len(vs) != 0 {
		t.Fatalf("affiliate sees videos not shared: %v", videoIDs(vs))
	}
	e.mustFail("ana", http.MethodGet, ws+"/videos/"+ref.ID.String(), nil, 404, "video_not_found")

	// The mentor shares the own video: the affiliate sees it on the product and downloads it.
	e.must("mentor", http.MethodPatch, ws+"/videos/"+own.ID.String(), map[string]any{"shared": true}, &own, http.StatusOK)
	if !own.Shared {
		t.Fatalf("not shared: %+v", own)
	}
	e.must("ana", http.MethodGet, ws+"/videos?product_id="+product.String(), nil, &vs, http.StatusOK)
	if len(vs) != 1 || vs[0].ID != own.ID || vs[0].Mine || vs[0].PreviewURL == nil || len(vs[0].ProductIDs) != 0 {
		t.Fatalf("videos of the product for the affiliate: %+v", vs)
	}
	e.must("ana", http.MethodGet, ws+"/videos?product_id="+product.String()+"&mine=true", nil, &vs, http.StatusOK)
	if len(vs) != 0 {
		t.Fatalf("mine for the affiliate: %+v", vs)
	}
	var dl signedURLJSON
	e.must("ana", http.MethodGet, ws+"/videos/"+own.ID.String()+"/download", nil, &dl, http.StatusOK)
	e.mustFail("ana", http.MethodPatch, ws+"/videos/"+own.ID.String(), map[string]any{"title": "mine"}, 403, "video_owner_only")
	e.mustFail("ana", http.MethodDelete, ws+"/videos/"+own.ID.String(), nil, 403, "video_owner_only")
	e.mustFail("ana", http.MethodPut, ws+"/videos/"+own.ID.String()+"/products/"+e.products[3].String(), nil, 403, "video_owner_only")
	e.mustFail("ana", http.MethodDelete, ws+"/videos/"+own.ID.String()+"/products/"+product.String(), nil, 403, "video_owner_only")

	// The affiliate does not share hers (not even in the personal workspace).
	var hers videoJSON
	e.must("ana", http.MethodPost, ws+"/videos/embed", map[string]any{"url": mediaYouTubeOK2}, &hers, http.StatusCreated)
	e.mustFail("ana", http.MethodPatch, ws+"/videos/"+hers.ID.String(), map[string]any{"shared": true}, 403, "video_share_managers_only")
	personal := e.personal("ana").ID
	var inPersonal videoJSON
	e.must("ana", http.MethodPost, wsPath(personal, "/videos/embed"), map[string]any{"url": mediaYouTubeOK2}, &inPersonal, http.StatusCreated)
	e.mustFail("ana", http.MethodPatch, wsPath(personal, "/videos/"+inPersonal.ID.String()), map[string]any{"shared": true}, 403, "video_share_managers_only")
	// Not even through the RLS, without the service.
	anaID := e.me("ana").ID
	err := database.InTx(ctx, e.pool, database.Scope{UserID: anaID.String(), WorkspaceID: wsID.String()}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE videos SET shared = true WHERE id = $1", hers.ID)
		return err
	})
	if !isRLSViolation(err) {
		t.Fatalf("RLS let the affiliate share: %v", err)
	}

	// The mentor attaches the reference to a list (curation): it becomes shared.
	mentor, ana := e.member("mentor", wsID), e.member("ana", wsID)
	list := uuid.New()
	if err := e.svcs.media.LinkList(ctx, mentor, ref.ID, list); err != nil {
		t.Fatal(err)
	}
	if err := e.svcs.media.LinkList(ctx, ana, hers.ID, list); !errors.Is(err, domain.ErrVideoShareManagers) {
		t.Fatalf("affiliate attached to a list: %v", err)
	}
	if err := e.svcs.media.LinkList(ctx, mentor, hers.ID, list); !errors.Is(err, domain.ErrVideoNotFound) {
		t.Fatalf("mentor attached someone else's unshared video: %v", err)
	}
	if err := e.svcs.media.LinkList(ctx, mentor, own.ID, list); err != nil {
		t.Fatal(err)
	}
	byList, err := e.svcs.media.ForTargets(ctx, ana, domain.TargetList, []uuid.UUID{list})
	if err != nil {
		t.Fatal(err)
	}
	if got := byList[list]; len(got) != 2 || got[0].ID != ref.ID || !got[0].Shared || got[1].ID != own.ID {
		t.Fatalf("list videos for the affiliate: %+v", got)
	}
	byProduct, err := e.svcs.media.ForTargets(ctx, ana, domain.TargetProduct, []uuid.UUID{product})
	if err != nil {
		t.Fatal(err)
	}
	if got := byProduct[product]; len(got) != 2 || got[0].ID != ref.ID || got[1].ID != own.ID {
		t.Fatalf("product videos for the affiliate: %+v", got)
	}
	e.must("mentor", http.MethodGet, ws+"/videos/"+ref.ID.String(), nil, &ref, http.StatusOK)
	if !ref.Shared || !slices.Equal(ref.ListIDs, []uuid.UUID{list}) {
		t.Fatalf("reference in the list: %+v", ref)
	}

	// Removing from the list; the affiliate cannot (RLS); deleting the list
	// takes the links, not the videos.
	if err := e.svcs.media.UnlinkList(ctx, ana, ref.ID, list); !errors.Is(err, domain.ErrVideoNotFound) {
		t.Fatalf("affiliate removed from a list: %v", err)
	}
	if err := e.svcs.media.UnlinkList(ctx, mentor, ref.ID, list); err != nil {
		t.Fatal(err)
	}
	if err := e.svcs.media.UnlinkList(ctx, mentor, ref.ID, list); !errors.Is(err, domain.ErrVideoNotFound) {
		t.Fatalf("removing twice: %v", err)
	}
	if err := e.svcs.media.UnlinkTarget(ctx, mentor, domain.TargetList, list); err != nil {
		t.Fatal(err)
	}
	e.must("mentor", http.MethodGet, ws+"/videos/"+own.ID.String(), nil, &own, http.StatusOK)
	if len(own.ListIDs) != 0 || len(own.ProductIDs) != 1 || !own.Shared {
		t.Fatalf("links after deleting the list: %+v", own)
	}
}

// TestMediaLeakAPI: videos and quota of one workspace do not show in
// another, not even to the same user.
func TestMediaLeakAPI(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	wsA := e.mentorship("mentor", "Turma").ID
	e.join("mentor", "ana", wsA)
	wsB := e.personal("beto").ID
	product := e.products[0]

	var ref videoJSON
	e.must("mentor", http.MethodPost, wsPath(wsA, "/videos/embed"), map[string]any{"url": mediaYouTubeOK, "product_id": product}, &ref, http.StatusCreated)
	v := e.upload("mentor", wsPath(wsA, ""), "a.mp4", bytes.Repeat([]byte("a"), 20), &product)
	e.run("process_video")
	e.must("mentor", http.MethodPatch, wsPath(wsA, "/videos/"+v.ID.String()), map[string]any{"shared": true}, &v, http.StatusOK)

	// beto is not a member of A.
	e.mustFail("beto", http.MethodGet, wsPath(wsA, "/videos/"+v.ID.String()), nil, 404, "workspace_not_found")
	e.mustFail("beto", http.MethodGet, wsPath(wsA, "/videos"), nil, 404, "workspace_not_found")
	for _, id := range []uuid.UUID{ref.ID, v.ID} {
		other := wsPath(wsB, "/videos/"+id.String())
		e.mustFail("beto", http.MethodGet, other, nil, 404, "video_not_found")
		e.mustFail("beto", http.MethodPatch, other, map[string]any{"title": "x"}, 404, "video_not_found")
		e.mustFail("beto", http.MethodDelete, other, nil, 404, "video_not_found")
		e.mustFail("beto", http.MethodGet, other+"/download", nil, 404, "video_not_found")
		e.mustFail("beto", http.MethodPost, other+"/parts", map[string]any{"number": 1}, 404, "video_not_found")
		e.mustFail("beto", http.MethodPut, other+"/products/"+product.String(), nil, 404, "video_not_found")
	}
	var vs []videoJSON
	e.must("beto", http.MethodGet, wsPath(wsB, "/videos?product_id="+product.String()), nil, &vs, http.StatusOK)
	if len(vs) != 0 {
		t.Fatalf("beto sees videos of another workspace on the product: %v", videoIDs(vs))
	}
	e.must("beto", http.MethodGet, wsPath(wsB, "/videos"), nil, &vs, http.StatusOK)
	if len(vs) != 0 {
		t.Fatalf("beto sees videos of another workspace: %v", videoIDs(vs))
	}
	// The mentor, in their personal workspace, does not see the mentorship's either.
	personal := e.personal("mentor").ID
	e.must("mentor", http.MethodGet, wsPath(personal, "/videos?product_id="+product.String()), nil, &vs, http.StatusOK)
	if len(vs) != 0 {
		t.Fatalf("mentorship videos in the personal workspace: %v", videoIDs(vs))
	}
	e.mustFail("mentor", http.MethodGet, wsPath(personal, "/videos/"+v.ID.String()), nil, 404, "video_not_found")
	var quota videoQuotaJSON
	e.must("mentor", http.MethodGet, wsPath(personal, "/videos/quota"), nil, &quota, http.StatusOK)
	if quota.UsedBytes != 0 {
		t.Fatalf("personal quota with mentorship usage: %+v", quota)
	}
	// Between users of the same workspace: ana sees only the shared upload,
	// without its links, and not the mentor's unshared reference.
	e.must("ana", http.MethodGet, wsPath(wsA, "/videos"), nil, &vs, http.StatusOK)
	if !slices.Equal(videoIDs(vs), []uuid.UUID{v.ID}) || len(vs[0].ProductIDs) != 0 {
		t.Fatalf("ana sees: %+v", vs)
	}
	e.mustFail("ana", http.MethodGet, wsPath(wsA, "/videos/"+ref.ID.String()+"/download"), nil, 404, "video_not_found")
}

// TestMediaLeakRLS: straight in the database, with the scope of another
// workspace or of another user, nothing of the videos shows or changes.
func TestMediaLeakRLS(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	ctx := context.Background()
	wsA := e.mentorship("mentor", "Turma").ID
	e.join("mentor", "ana", wsA)
	wsB := e.personal("beto").ID
	product := e.products[0]
	var ref videoJSON
	e.must("mentor", http.MethodPost, wsPath(wsA, "/videos/embed"), map[string]any{"url": mediaYouTubeOK, "product_id": product}, &ref, http.StatusCreated)
	e.upload("mentor", wsPath(wsA, ""), "a.mp4", bytes.Repeat([]byte("a"), 20), nil)

	count := func(scope database.Scope) map[string]int {
		out := map[string]int{}
		err := database.InTx(ctx, e.pool, scope, func(tx pgx.Tx) error {
			for _, table := range []string{"videos", "video_links", "video_usage"} {
				var n int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
					return err
				}
				out[table] = n
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	betoID, anaID := e.me("beto").ID, e.me("ana").ID
	// Another workspace (beto's own scope, and beto pretending to be in A).
	for _, s := range []database.Scope{
		{UserID: betoID.String(), WorkspaceID: wsB.String()},
		{UserID: e.me("mentor").ID.String(), WorkspaceID: wsB.String()},
	} {
		if got := count(s); got["videos"] != 0 || got["video_links"] != 0 || got["video_usage"] != 0 {
			t.Fatalf("scope %+v sees %v", s, got)
		}
	}
	// Another user of the same workspace: the usage of the workspace, but no
	// unshared video nor its links.
	if got := count(database.Scope{UserID: anaID.String(), WorkspaceID: wsA.String()}); got["videos"] != 0 || got["video_links"] != 0 || got["video_usage"] != 1 {
		t.Fatalf("ana sees %v", got)
	}

	// Writes with another scope change nothing or are refused.
	err := database.InTx(ctx, e.pool, database.Scope{UserID: anaID.String(), WorkspaceID: wsA.String()}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE videos SET title = 'x' WHERE id = $1", ref.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			return fmt.Errorf("updated %d videos of the mentor", tag.RowsAffected())
		}
		if tag, err = tx.Exec(ctx, "DELETE FROM videos WHERE id = $1", ref.ID); err != nil || tag.RowsAffected() != 0 {
			return fmt.Errorf("deleted the mentor's video: %v %v", tag, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = database.InTx(ctx, e.pool, database.Scope{UserID: betoID.String(), WorkspaceID: wsB.String()}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO video_links (video_id, workspace_id, owner_id, target_kind, target_id)
			VALUES ($1, $2, $3, 'product', $4)`, ref.ID, wsA, betoID, product)
		return err
	})
	if !isRLSViolation(err) {
		t.Fatalf("beto linked a video of another workspace: %v", err)
	}
	err = database.InTx(ctx, e.pool, database.Scope{UserID: betoID.String(), WorkspaceID: wsB.String()}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO videos (workspace_id, owner_id, kind, platform, status, url, embed_id)
			VALUES ($1, $2, 'embed', 'youtube', 'ready', 'u', 'e')`, wsA, betoID)
		return err
	})
	if !isRLSViolation(err) {
		t.Fatalf("beto created a video in another workspace: %v", err)
	}
}

// TestMediaRiverQueue enqueues the media jobs in a real River, which checks
// the options of each: processing does not enter twice, and the revalidation
// can schedule its own next run.
func TestMediaRiverQueue(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := queue.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := queue.NewInsertClient(pool, mediaWorkerLog())
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.River{Client: client}
	j := domain.VideoJob{VideoID: uuid.New(), WorkspaceID: uuid.New(), OwnerID: uuid.New()}
	later := time.Now().Add(time.Hour)
	for _, fn := range []func() error{
		func() error { return q.EnqueueProcessVideo(ctx, j) },
		func() error { return q.ScheduleCleanUpload(ctx, j, later) },
		func() error { return q.ScheduleRevalidateEmbed(ctx, j, later) },
		func() error { return q.EnqueueProcessVideo(ctx, j) },
		func() error { return q.ScheduleRevalidateEmbed(ctx, j, later.Add(time.Hour)) },
	} {
		if err := fn(); err != nil {
			t.Fatal(err)
		}
	}
	countJobs := func(kind, state, queueName string) (n int) {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = $1 AND state::text = $2 AND queue = $3",
			kind, state, queueName).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, c := range []struct {
		kind, state, queue string
		want               int
	}{
		{"process_video", "available", queue.QueueMedia, 1},
		{"clean_upload", "scheduled", queue.QueueMedia, 1},
		{"revalidate_embed", "scheduled", queue.QueueDefault, 2},
	} {
		if n := countJobs(c.kind, c.state, c.queue); n != c.want {
			t.Errorf("%s %s: %d, want %d", c.kind, c.state, n, c.want)
		}
	}
	var args map[string]any
	if err := pool.QueryRow(ctx, "SELECT args FROM river_job WHERE kind = 'process_video'").Scan(&args); err != nil {
		t.Fatal(err)
	}
	if args["video_id"] != j.VideoID.String() || args["workspace_id"] != j.WorkspaceID.String() || args["owner_id"] != j.OwnerID.String() {
		t.Fatalf("args: %v", args)
	}
}
