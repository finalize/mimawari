// Package server は見回りの結果を HTTP で返す。
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/finalize/mimawari/internal/cache"
	"github.com/finalize/mimawari/internal/model"
	"github.com/finalize/mimawari/internal/report"
)

// New はルーティング済みのハンドラを返す。
func New(c *cache.Cache) http.Handler {
	mux := http.NewServeMux()
	s := &server{cache: c}
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /status/{project}", s.project)
	mux.HandleFunc("GET /attention", s.attention)
	mux.HandleFunc("GET /", s.index)
	return mux
}

type server struct{ cache *cache.Cache }

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join([]string{
		"mimawari — 個人開発の見回り",
		"",
		"  GET /status                全プロジェクト（JSON）",
		"  GET /status?format=text    端末で読む形",
		"  GET /status?fresh=1        キャッシュを無視して取り直す",
		"  GET /status/{project}      1プロジェクトだけ",
		"  GET /attention             手を動かすところだけ",
		"  GET /healthz",
		"",
	}, "\n")))
}

// snapshot はキャッシュから結果を取り、取り直したかどうかをヘッダに出す。
func (s *server) snapshot(w http.ResponseWriter, r *http.Request) model.Snapshot {
	fresh, _ := strconv.ParseBool(r.URL.Query().Get("fresh"))
	snap, cached := s.cache.Get(r.Context(), fresh)
	if cached {
		w.Header().Set("X-Mimawari-Cache", "hit")
	} else {
		w.Header().Set("X-Mimawari-Cache", "miss")
	}
	return snap
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot(w, r)
	if wantsText(r) {
		writeText(w, report.Text(snap, false))
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) attention(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot(w, r)
	if wantsText(r) {
		var b strings.Builder
		for _, it := range snap.Attention {
			b.WriteString(string(it.Severity) + "\t" + it.Project + "\t" + it.Summary + "\n")
		}
		writeText(w, b.String())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": snap.GeneratedAt,
		"attention":    snap.Attention,
	})
}

func (s *server) project(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("project")
	snap := s.snapshot(w, r)
	for _, p := range snap.Projects {
		if p.Name != name {
			continue
		}
		one := snap
		one.Projects = []model.Project{p}
		one.Attention = filterByProject(snap.Attention, name)
		one.Errors = filterErrors(snap.Errors, p.Repo, p.Name)
		if wantsText(r) {
			writeText(w, report.Text(one, false))
			return
		}
		writeJSON(w, http.StatusOK, one)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{
		"error": "そのプロジェクトは設定にありません: " + name,
	})
}

func filterByProject(items []model.Item, name string) []model.Item {
	out := []model.Item{}
	for _, it := range items {
		if it.Project == name {
			out = append(out, it)
		}
	}
	return out
}

func filterErrors(errs []model.SourceError, repo, name string) []model.SourceError {
	out := []model.SourceError{}
	for _, e := range errs {
		if strings.Contains(e.Source, repo) || strings.Contains(e.Source, name) {
			out = append(out, e)
		}
	}
	return out
}

// wantsText は端末向けの出力を求められているかを返す。
func wantsText(r *http.Request) bool {
	if f := r.URL.Query().Get("format"); f != "" {
		return f == "text" || f == "txt"
	}
	accept := r.Header.Get("Accept")
	// ブラウザは text/html を先に並べつつ */* も付けてくるので、
	// text/plain を明示している要求だけを端末向けとみなす。
	return strings.Contains(accept, "text/plain")
}

func writeText(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
