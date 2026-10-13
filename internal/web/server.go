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

//go:embed static/copy.js
var copyScript []byte

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
	mux.HandleFunc("GET /copy.js", app.copyScript)
	mux.HandleFunc("GET /prompts/new", app.newForm)
	mux.HandleFunc("GET /prompts/draft", app.draftForm)
	mux.HandleFunc("POST /prompts/draft", app.guard(app.createDraft))
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

var beijingTime = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return loc
}()

func addedOn(at time.Time) string {
	return "添加于 " + at.In(beijingTime).Format("2006-01-02")
}

func exampleLabel(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return raw
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	segment := ""
	if path := strings.Trim(parsed.EscapedPath(), "/"); path != "" {
		segment, _, _ = strings.Cut(path, "/")
	}
	if host == "x.com" || host == "twitter.com" || host == "mobile.twitter.com" {
		if segment != "" && segment != "i" && !strings.EqualFold(segment, "status") {
			return "x.com/" + segment
		}
		return "x.com"
	}
	if segment == "" {
		return host
	}
	return host + "/" + strings.Trim(parsed.EscapedPath(), "/")
}

func (a *App) listEntry(item store.Prompt, excerpt template.HTML) listItem {
	return listItem{
		ID:           item.ID,
		Excerpt:      excerpt,
		ExampleURL:   item.ExampleURL,
		ExampleLabel: exampleLabel(item.ExampleURL),
		Body:         item.Body,
		Added:        addedOn(item.CreatedAt),
	}
}

func (a *App) copyScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(copyScript)
}

type promptWriter interface {
	DraftReady() bool
	Draft(ctx context.Context, need string, samples []string) (string, error)
}

func (a *App) draftForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "draft", draftPage{Title: "写一条"})
}

func (a *App) createDraft(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.fail(w, err)
		return
	}
	need := r.PostFormValue("need")
	if strings.TrimSpace(need) == "" {
		a.render(w, http.StatusBadRequest, "draft", draftPage{Title: "写一条", Need: need, Error: "要写的内容必填"})
		return
	}
	writer, ok := a.deriver.(promptWriter)
	if !ok || !writer.DraftReady() {
		a.render(w, http.StatusOK, "draft", draftPage{Title: "写一条", Need: need, Error: "还不能生成"})
		return
	}
	samples := a.writingSamples(r.Context(), need)
	text, err := writer.Draft(r.Context(), need, samples)
	if err != nil || strings.TrimSpace(text) == "" {
		if err != nil && a.deriver != nil {
			log.Printf("draft failed: %s", a.deriver.Redact(err.Error()))
		}
		a.render(w, http.StatusBadGateway, "draft", draftPage{Title: "写一条", Need: need, Error: "没有生成出来"})
		return
	}
	notice := "这是生成的草稿，改完再保存"
	if len(samples) == 0 {
		notice += "这次没有参考"
	}
	log.Printf("draft: runes=%d examples=%d", utf8.RuneCountInString(strings.TrimSpace(need)), len(samples))
	a.render(w, http.StatusOK, "form", formPage{
		Title:     "贴上提示词",
		Action:    "/prompts",
		Body:      text,
		Notice:    notice,
		CancelURL: "/",
	})
}

func (a *App) writingSamples(ctx context.Context, need string) []string {
	if a.deriver == nil || !a.deriver.EmbedReady() {
		return nil
	}
	items, err := a.store.References(need, a.min, func(text string) ([]float32, string, error) {
		values, model, _, err := a.queryCache.embed(ctx, text, a.deriver.EmbedQuery)
		return values, model, err
	})
	if err != nil {
		log.Printf("draft examples: %v", err)
		return nil
	}
	samples := make([]string, 0, len(items))
	for _, item := range items {
		samples = append(samples, item.Body)
	}
	return samples
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
	ID           string
	Excerpt      template.HTML
	ExampleURL   string
	ExampleLabel string
	Body         string
	Added        string
}

type listPage struct {
	Title       string
	Refresh     int
	ShowCount   bool
	Count       int
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
	ShowCount  bool
	Count      int
	Action     string
	Body       string
	ExampleURL string
	Error      string
	CancelURL  string
	Notice     string
}

type draftPage struct {
	Title     string
	Refresh   int
	ShowCount bool
	Count     int
	Need      string
	Error     string
}

type detailPage struct {
	Title      string
	Refresh    int
	ShowCount  bool
	Count      int
	ID         string
	Body       template.HTML
	CopyBody   string
	ExampleURL string
}

type failedItem struct {
	ID      string
	Excerpt string
	Reason  string
	When    string
}

type failedPage struct {
	Title     string
	Refresh   int
	ShowCount bool
	Count     int
	Empty     bool
	Working   bool
	Items     []failedItem
}

type deletePage struct {
	Title     string
	Refresh   int
	ShowCount bool
	Count     int
	ID        string
	Excerpt   string
}

type messagePage struct {
	Title     string
	Refresh   int
	ShowCount bool
	Count     int
	Message   string
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
	page.Count, err = a.store.Count()
	if err != nil {
		a.fail(w, err)
		return
	}
	page.ShowCount = true
	if strings.TrimSpace(query) == "" {
		items, err := a.store.List()
		if err != nil {
			a.fail(w, err)
			return
		}
		for _, item := range items {
			page.Prompts = append(page.Prompts, a.listEntry(item, template.HTML(html.EscapeString(prompt.Excerpt(item.Body)))))
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
		page.Prompts = append(page.Prompts, a.listEntry(hit.Prompt, excerptHTML(hit.Prompt.Body, hit.Spans)))
	}
	for _, hit := range match.Meaning {
		page.Meaning = append(page.Meaning, a.listEntry(hit.Prompt, excerptHTML(hit.Prompt.Body, hit.Spans)))
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
		CopyBody:   item.Body,
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
