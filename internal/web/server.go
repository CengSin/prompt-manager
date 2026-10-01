package web

import (
	"context"
	"embed"
	"errors"
	"html"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/prompt"
	"prompt-manager/internal/store"
)

//go:embed templates/*.html
var templateFiles embed.FS

func ListenAddr(port string) string {
	if port == "" {
		port = "8787"
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func NewHandler(st *store.Store) http.Handler {
	return New(st, nil)
}

func New(st *store.Store, deriver Deriver) *App {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		store:         st,
		tmpl:          template.Must(template.ParseFS(templateFiles, "templates/*.html")),
		deriver:       deriver,
		jobs:          make(chan job),
		ctx:           ctx,
		cancel:        cancel,
		min:           0.35,
		limit:         5,
		shortQueryMin: derive.DefaultShortQuerySimilarity,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.list)
	mux.HandleFunc("GET /prompts/new", app.newForm)
	mux.HandleFunc("POST /prompts", app.guard(app.create))
	mux.HandleFunc("GET /prompts/failed", app.failed)
	mux.HandleFunc("POST /prompts/failed/retry", app.guard(app.retryAll))
	mux.HandleFunc("GET /prompts/{id}", app.detail)
	mux.HandleFunc("GET /prompts/{id}/edit", app.editForm)
	mux.HandleFunc("POST /prompts/{id}", app.guard(app.update))
	mux.HandleFunc("POST /prompts/{id}/derive", app.guard(app.retryOne))
	mux.HandleFunc("GET /prompts/{id}/delete", app.deleteConfirm)
	mux.HandleFunc("POST /prompts/{id}/delete", app.guard(app.delete))
	app.mux = mux
	return app
}

type Deriver interface {
	Ready() bool
	EmbedReady() bool
	Derive(ctx context.Context, body string) (derive.Result, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, string, error)
	Redact(reason string) string
}

type job struct {
	id  string
	gen int
}

type App struct {
	store         *store.Store
	tmpl          *template.Template
	deriver       Deriver
	jobs          chan job
	ctx           context.Context
	cancel        context.CancelFunc
	mux           *http.ServeMux
	min           float64
	limit         int
	queryCache    queryCache
	shortQueryMin float64
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

func (a *App) Start() {
	if a.deriver == nil {
		return
	}
	go a.loop()
}

func (a *App) Stop() {
	a.cancel()
}

func (a *App) Backfill() {
	if a.deriver == nil || !a.deriver.Ready() {
		return
	}
	items, err := a.store.ListUnprocessed()
	if err != nil {
		log.Printf("backfill: %v", err)
		return
	}
	for _, item := range items {
		a.enqueue(item.ID)
	}
}

func (a *App) SetLimits(min float64, limit int) {
	a.min = min
	a.limit = limit
}

func (a *App) SetShortQueryMinimum(min float64) {
	a.shortQueryMin = min
}

func (a *App) queryMinimum(query string) float64 {
	length := utf8.RuneCountInString(strings.TrimSpace(query))
	if length > 0 && length <= 4 && a.shortQueryMin > a.min {
		return a.shortQueryMin
	}
	return a.min
}

func (a *App) loop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case job := <-a.jobs:
			a.run(job)
		}
	}
}

func (a *App) enqueue(id string) {
	if a.deriver == nil || !a.deriver.Ready() {
		return
	}
	item, err := a.store.Get(id)
	if err != nil {
		return
	}
	if err := a.store.MarkPending(id, item.Generation); err != nil {
		return
	}
	gen := item.Generation
	go func() {
		select {
		case a.jobs <- job{id: id, gen: gen}:
		case <-a.ctx.Done():
		}
	}()
}

func (a *App) run(job job) {
	item, err := a.store.Get(job.id)
	if err != nil || item.Generation != job.gen {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()
	result, err := a.deriver.Derive(ctx, item.Body)
	if errors.Is(err, derive.ErrNotConfigured) {
		_ = a.store.MarkIdle(job.id, job.gen)
		return
	}
	if err != nil {
		reason := a.deriver.Redact(err.Error())
		log.Printf("derive %s: %s", job.id, reason)
		if markErr := a.store.MarkFailed(job.id, job.gen, reason); markErr != nil {
			log.Printf("derive %s mark failed: %v", job.id, markErr)
		}
		return
	}
	if err := a.store.CommitDerived(job.id, job.gen, result); err != nil && !errors.Is(err, store.ErrStale) {
		reason := a.deriver.Redact(err.Error())
		log.Printf("save derived data: %s", reason)
		_ = a.store.MarkFailed(job.id, job.gen, reason)
	}
}

type listItem struct {
	ID         string
	Excerpt    template.HTML
	ExampleURL string
}

type listPage struct {
	Title       string
	Refresh     int
	Query       string
	Prompts     []listItem
	Meaning     []listItem
	FailedCount int
	Empty       bool
	NoMatch     bool
}

type formPage struct {
	Title      string
	Refresh    int
	Action     string
	Body       string
	ExampleURL string
	Error      string
	CancelURL  string
}

type detailPage struct {
	Title      string
	Refresh    int
	ID         string
	Body       template.HTML
	ExampleURL string
}

type failedItem struct {
	ID      string
	Excerpt string
	Reason  string
	When    string
}

type failedPage struct {
	Title   string
	Refresh int
	Empty   bool
	Working bool
	Items   []failedItem
}

type deletePage struct {
	Title   string
	Refresh int
	ID      string
	Excerpt string
}

type messagePage struct {
	Title   string
	Refresh int
	Message string
}

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	page := listPage{Title: "提示词", Query: query}
	var err error
	page.FailedCount, err = a.store.CountFailed()
	if err != nil {
		a.fail(w, err)
		return
	}
	if strings.TrimSpace(query) == "" {
		items, err := a.store.List()
		if err != nil {
			a.fail(w, err)
			return
		}
		for _, item := range items {
			page.Prompts = append(page.Prompts, listItem{
				ID:         item.ID,
				Excerpt:    template.HTML(html.EscapeString(prompt.Excerpt(item.Body))),
				ExampleURL: item.ExampleURL,
			})
		}
		page.Empty = len(items) == 0
		a.render(w, http.StatusOK, "list", page)
		return
	}
	match, err := a.match(r.Context(), query)
	if err != nil {
		a.fail(w, err)
		return
	}
	for _, hit := range match.Literal {
		page.Prompts = append(page.Prompts, listItem{
			ID:         hit.Prompt.ID,
			Excerpt:    excerptHTML(hit.Prompt.Body, hit.Spans),
			ExampleURL: hit.Prompt.ExampleURL,
		})
	}
	for _, hit := range match.Meaning {
		page.Meaning = append(page.Meaning, listItem{
			ID:         hit.Prompt.ID,
			Excerpt:    excerptHTML(hit.Prompt.Body, hit.Spans),
			ExampleURL: hit.Prompt.ExampleURL,
		})
	}
	page.NoMatch = len(page.Prompts) == 0 && len(page.Meaning) == 0
	a.render(w, http.StatusOK, "list", page)
}

func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "form", formPage{
		Title:     "贴上提示词",
		Action:    "/prompts",
		CancelURL: "/",
	})
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	body, exampleURL := readPromptForm(r)
	item, err := a.store.Create(body, exampleURL)
	if err != nil {
		if formErr(err) {
			a.render(w, http.StatusBadRequest, "form", formPage{
				Title:      "贴上提示词",
				Action:     "/prompts",
				Body:       body,
				ExampleURL: exampleURL,
				Error:      err.Error(),
				CancelURL:  "/",
			})
			return
		}
		a.fail(w, err)
		return
	}
	a.enqueue(item.ID)
	http.Redirect(w, r, "/prompts/"+item.ID, http.StatusSeeOther)
}

func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	item, ok := a.load(w, r.PathValue("id"))
	if !ok {
		return
	}
	query := r.URL.Query().Get("q")
	body := template.HTML(html.EscapeString(item.Body))
	if strings.TrimSpace(query) != "" {
		match, err := a.match(r.Context(), query)
		if err != nil {
			a.fail(w, err)
			return
		}
		for _, hit := range append(match.Literal, match.Meaning...) {
			if hit.Prompt.ID == item.ID && len(hit.Spans) > 0 {
				body = markHTML(item.Body, hit.Spans)
				break
			}
		}
	}
	a.render(w, http.StatusOK, "detail", detailPage{
		Title:      "提示词",
		ID:         item.ID,
		Body:       body,
		ExampleURL: item.ExampleURL,
	})
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	item, ok := a.load(w, r.PathValue("id"))
	if !ok {
		return
	}
	a.render(w, http.StatusOK, "form", formPage{
		Title:      "改正提示词",
		Action:     "/prompts/" + item.ID,
		Body:       item.Body,
		ExampleURL: item.ExampleURL,
		CancelURL:  "/prompts/" + item.ID,
	})
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	current, err := a.store.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w)
			return
		}
		a.fail(w, err)
		return
	}
	body, exampleURL := readPromptForm(r)
	item, err := a.store.Update(id, body, exampleURL)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w)
			return
		}
		if formErr(err) {
			a.render(w, http.StatusBadRequest, "form", formPage{
				Title:      "改正提示词",
				Action:     "/prompts/" + id,
				Body:       body,
				ExampleURL: exampleURL,
				Error:      err.Error(),
				CancelURL:  "/prompts/" + id,
			})
			return
		}
		a.fail(w, err)
		return
	}
	if current.Body != item.Body {
		a.enqueue(item.ID)
	}
	http.Redirect(w, r, "/prompts/"+item.ID, http.StatusSeeOther)
}

func (a *App) deleteConfirm(w http.ResponseWriter, r *http.Request) {
	item, ok := a.load(w, r.PathValue("id"))
	if !ok {
		return
	}
	a.render(w, http.StatusOK, "delete", deletePage{
		Title:   "删除提示词",
		ID:      item.ID,
		Excerpt: prompt.Excerpt(item.Body),
	})
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w)
			return
		}
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) failed(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListFailed()
	if err != nil {
		a.fail(w, err)
		return
	}
	pending, err := a.store.CountPending()
	if err != nil {
		a.fail(w, err)
		return
	}
	page := failedPage{Title: "处理失败", Empty: len(items) == 0, Working: pending > 0}
	if pending > 0 {
		page.Refresh = 2
	}
	for _, item := range items {
		page.Items = append(page.Items, failedItem{
			ID:      item.ID,
			Excerpt: prompt.Excerpt(item.Body),
			Reason:  item.Reason,
			When:    item.FailedAt.UTC().Format("2006-01-02 15:04:05"),
		})
	}
	a.render(w, http.StatusOK, "failed", page)
}

func (a *App) retryOne(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := a.store.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w)
			return
		}
		a.fail(w, err)
		return
	}
	if item.Status == store.StatusFailed && a.deriver != nil && a.deriver.Ready() {
		a.enqueue(id)
	}
	http.Redirect(w, r, "/prompts/failed", http.StatusSeeOther)
}

func (a *App) retryAll(w http.ResponseWriter, r *http.Request) {
	if a.deriver != nil && a.deriver.Ready() {
		items, err := a.store.ListFailed()
		if err != nil {
			a.fail(w, err)
			return
		}
		for _, item := range items {
			a.enqueue(item.ID)
		}
	}
	http.Redirect(w, r, "/prompts/failed", http.StatusSeeOther)
}

func (a *App) match(ctx context.Context, query string) (store.Match, error) {
	started := time.Now()
	var embedDuration time.Duration
	var cached bool
	defer func() {
		log.Printf("search: query_runes=%d embedding=%s cached=%t total=%s", utf8.RuneCountInString(strings.TrimSpace(query)), embedDuration.Round(time.Millisecond), cached, time.Since(started).Round(time.Millisecond))
	}()
	return a.store.Match(query, store.MatchConfig{
		Min:   a.queryMinimum(query),
		Limit: a.limit,
		Embed: func(text string) ([]float32, string, error) {
			if a.deriver == nil || !a.deriver.EmbedReady() {
				return nil, "", derive.ErrNotConfigured
			}
			started := time.Now()
			values, model, hit, err := a.queryCache.embed(ctx, text, a.deriver.EmbedQuery)
			embedDuration = time.Since(started)
			cached = hit
			if err != nil {
				log.Printf("query embedding failed: %s", a.deriver.Redact(err.Error()))
			}
			return values, model, err
		},
	})
}

func (a *App) load(w http.ResponseWriter, id string) (store.Prompt, bool) {
	item, err := a.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w)
		return store.Prompt{}, false
	}
	if err != nil {
		a.fail(w, err)
		return store.Prompt{}, false
	}
	return item, true
}

func (a *App) notFound(w http.ResponseWriter) {
	a.render(w, http.StatusNotFound, "message", messagePage{
		Title:   "未找到",
		Message: "未找到这条提示词",
	})
}

func (a *App) fail(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	http.Error(w, "保存失败", http.StatusInternalServerError)
}

func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (a *App) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "拒绝来自其他网站的提交", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func sameOrigin(r *http.Request) bool {
	raw := r.Header.Get("Origin")
	if raw == "" {
		raw = r.Header.Get("Referer")
	}
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func readPromptForm(r *http.Request) (string, string) {
	_ = r.ParseForm()
	return r.PostFormValue("body"), r.PostFormValue("example_url")
}

func formErr(err error) bool {
	return errors.Is(err, prompt.ErrBodyRequired) || errors.Is(err, prompt.ErrInvalidURL)
}
