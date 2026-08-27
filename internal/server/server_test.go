package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/finalize/mimawari/internal/cache"
	"github.com/finalize/mimawari/internal/model"
)

func fixture() model.Snapshot {
	return model.Snapshot{
		GeneratedAt: time.Date(2026, 8, 27, 22, 0, 0, 0, time.UTC),
		TookMS:      1200,
		Projects: []model.Project{
			{Name: "termpic", Repo: "finalize/termpic", Pulls: []model.PullRequest{{Number: 1}}},
			{Name: "hidori", Repo: "finalize/hidori"},
		},
		Attention: []model.Item{
			{Project: "termpic", Kind: "pr-checks-failing", Severity: model.SevAction, Summary: "PR #1 が落ちている"},
			{Project: "hidori", Kind: "site-down", Severity: model.SevAction, Summary: "サイトが応答していない"},
		},
		Errors: []model.SourceError{
			{Source: "npm:termpic", Message: "429"},
			{Source: "github:finalize/hidori", Message: "500"},
		},
	}
}

func newTestServer(t *testing.T) (http.Handler, *int) {
	t.Helper()
	calls := 0
	c := cache.New(time.Hour, func(context.Context) model.Snapshot {
		calls++
		return fixture()
	})
	return New(c), &calls
}

func do(t *testing.T, h http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStatusJSON(t *testing.T) {
	h, calls := newTestServer(t)
	rec := do(t, h, "/status", nil)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := rec.Header().Get("X-Mimawari-Cache"); got != "miss" {
		t.Errorf("初回は miss のはず: %q", got)
	}

	var snap model.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("JSON を読めません: %v", err)
	}
	if len(snap.Projects) != 2 || len(snap.Attention) != 2 {
		t.Errorf("中身 = %+v", snap)
	}

	rec = do(t, h, "/status", nil)
	if got := rec.Header().Get("X-Mimawari-Cache"); got != "hit" {
		t.Errorf("2回目は hit のはず: %q", got)
	}
	if *calls != 1 {
		t.Errorf("取得回数 = %d", *calls)
	}
}

func TestStatusFresh(t *testing.T) {
	h, calls := newTestServer(t)
	do(t, h, "/status", nil)
	rec := do(t, h, "/status?fresh=1", nil)
	if got := rec.Header().Get("X-Mimawari-Cache"); got != "miss" {
		t.Errorf("fresh=1 は取り直すはず: %q", got)
	}
	if *calls != 2 {
		t.Errorf("取得回数 = %d, 期待は 2", *calls)
	}
}

func TestStatusText(t *testing.T) {
	h, _ := newTestServer(t)
	for _, target := range []string{"/status?format=text", "/status?format=txt"} {
		rec := do(t, h, target, nil)
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("%s の Content-Type = %q", target, ct)
		}
		if !strings.Contains(rec.Body.String(), "手を動かすところ") {
			t.Errorf("%s の本文 = %q", target, rec.Body.String())
		}
	}

	// Accept で明示したときも端末向けにする。
	rec := do(t, h, "/status", map[string]string{"Accept": "text/plain"})
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Error("Accept: text/plain が効いていません")
	}
	// ブラウザは text/html を先に並べるので JSON のままにする。
	rec = do(t, h, "/status", map[string]string{"Accept": "text/html,application/xhtml+xml,*/*"})
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Error("ブラウザの Accept で text になっています")
	}
}

func TestProject(t *testing.T) {
	h, _ := newTestServer(t)
	rec := do(t, h, "/status/termpic", nil)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var snap model.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Projects) != 1 || snap.Projects[0].Name != "termpic" {
		t.Fatalf("projects = %+v", snap.Projects)
	}
	if len(snap.Attention) != 1 || snap.Attention[0].Project != "termpic" {
		t.Errorf("attention が絞られていません: %+v", snap.Attention)
	}
	if len(snap.Errors) != 1 || snap.Errors[0].Source != "npm:termpic" {
		t.Errorf("errors が絞られていません: %+v", snap.Errors)
	}
}

func TestProjectNotFound(t *testing.T) {
	h, _ := newTestServer(t)
	rec := do(t, h, "/status/知らない子", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, 期待は 404", rec.Code)
	}
}

func TestAttention(t *testing.T) {
	h, _ := newTestServer(t)
	rec := do(t, h, "/attention", nil)
	var body struct {
		Attention []model.Item `json:"attention"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Attention) != 2 {
		t.Errorf("attention = %+v", body.Attention)
	}

	rec = do(t, h, "/attention?format=text", nil)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("行数 = %d: %q", len(lines), rec.Body.String())
	}
	if got := strings.Split(lines[0], "\t"); len(got) != 3 || got[0] != "action" || got[1] != "termpic" {
		t.Errorf("1行目 = %q", lines[0])
	}
}

func TestHealthzAndIndex(t *testing.T) {
	h, _ := newTestServer(t)
	if rec := do(t, h, "/healthz", nil); rec.Code != 200 || rec.Body.String() != "ok\n" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, "/", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "/status") {
		t.Errorf("index = %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, "/そんなものはない", nil); rec.Code != http.StatusNotFound {
		t.Errorf("未知のパス = %d, 期待は 404", rec.Code)
	}
}
