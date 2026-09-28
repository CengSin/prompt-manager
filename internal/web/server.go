package web

import (
	"embed"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

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
	h := &handler{
		store: st,
		tmpl:  template.Must(template.ParseFS(templateFiles, "templates/*.html")),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.list)
	mux.HandleFunc("GET /prompts/new", h.newForm)
	mux.HandleFunc("POST /prompts", h.guard(h.create))
	mux.HandleFunc("GET /prompts/{id}", h.detail)
	mux.HandleFunc("GET /prompts/{id}/edit", h.editForm)
	mux.HandleFunc("POST /prompts/{id}", h.guard(h.update))
	mux.HandleFunc("GET /prompts/{id}/delete", h.deleteConfirm)
	mux.HandleFunc("POST /prompts/{id}/delete", h.guard(h.delete))
	return mux
}

type handler struct {
	store *store.Store
	tmpl  *template.Template
}

type listItem struct {
	ID         string
	Excerpt    string
	ExampleURL string
}

type listPage struct {
	Title   string
	Query   string
	Prompts []listItem
	Empty   bool
	NoMatch bool
}

type formPage struct {
	Title      string
	Action     string
	Body       string
	ExampleURL string
	Error      string
	CancelURL  string
}

type detailPage struct {
	Title      string
	ID         string
	Body       string
	ExampleURL string
}

type deletePage struct {
	Title   string
	ID      string
	Excerpt string
}

type messagePage struct {
	Title   string
	Message string
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	items, err := h.store.Search(query)
	if err != nil {
		h.fail(w, err)
		return
	}
	page := listPage{Title: "提示词", Query: query}
	for _, item := range items {
		page.Prompts = append(page.Prompts, listItem{
			ID:         item.ID,
			Excerpt:    prompt.Excerpt(item.Body),
			ExampleURL: item.ExampleURL,
		})
	}
	if len(items) == 0 {
		if strings.TrimSpace(query) == "" {
			page.Empty = true
		} else {
			page.NoMatch = true
		}
	}
	h.render(w, http.StatusOK, "list", page)
}

func (h *handler) newForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, "form", formPage{
		Title:     "贴上提示词",
		Action:    "/prompts",
		CancelURL: "/",
	})
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	body, exampleURL := readPromptForm(r)
	item, err := h.store.Create(body, exampleURL)
	if err != nil {
		if formErr(err) {
			h.render(w, http.StatusBadRequest, "form", formPage{
				Title:      "贴上提示词",
				Action:     "/prompts",
				Body:       body,
				ExampleURL: exampleURL,
				Error:      err.Error(),
				CancelURL:  "/",
			})
			return
		}
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/prompts/"+item.ID, http.StatusSeeOther)
}

func (h *handler) detail(w http.ResponseWriter, r *http.Request) {
	item, ok := h.load(w, r.PathValue("id"))
	if !ok {
		return
	}
	h.render(w, http.StatusOK, "detail", detailPage{
		Title:      "提示词",
		ID:         item.ID,
		Body:       item.Body,
		ExampleURL: item.ExampleURL,
	})
}

func (h *handler) editForm(w http.ResponseWriter, r *http.Request) {
	item, ok := h.load(w, r.PathValue("id"))
	if !ok {
		return
	}
	h.render(w, http.StatusOK, "form", formPage{
		Title:      "改正提示词",
		Action:     "/prompts/" + item.ID,
		Body:       item.Body,
		ExampleURL: item.ExampleURL,
		CancelURL:  "/prompts/" + item.ID,
	})
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body, exampleURL := readPromptForm(r)
	item, err := h.store.Update(id, body, exampleURL)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.notFound(w)
			return
		}
		if formErr(err) {
			h.render(w, http.StatusBadRequest, "form", formPage{
				Title:      "改正提示词",
				Action:     "/prompts/" + id,
				Body:       body,
				ExampleURL: exampleURL,
				Error:      err.Error(),
				CancelURL:  "/prompts/" + id,
			})
			return
		}
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/prompts/"+item.ID, http.StatusSeeOther)
}

func (h *handler) deleteConfirm(w http.ResponseWriter, r *http.Request) {
	item, ok := h.load(w, r.PathValue("id"))
	if !ok {
		return
	}
	h.render(w, http.StatusOK, "delete", deletePage{
		Title:   "删除提示词",
		ID:      item.ID,
		Excerpt: prompt.Excerpt(item.Body),
	})
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.notFound(w)
			return
		}
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) load(w http.ResponseWriter, id string) (store.Prompt, bool) {
	item, err := h.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		h.notFound(w)
		return store.Prompt{}, false
	}
	if err != nil {
		h.fail(w, err)
		return store.Prompt{}, false
	}
	return item, true
}

func (h *handler) notFound(w http.ResponseWriter) {
	h.render(w, http.StatusNotFound, "message", messagePage{
		Title:   "未找到",
		Message: "未找到这条提示词",
	})
}

func (h *handler) fail(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	http.Error(w, "保存失败", http.StatusInternalServerError)
}

func (h *handler) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (h *handler) guard(next http.HandlerFunc) http.HandlerFunc {
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
