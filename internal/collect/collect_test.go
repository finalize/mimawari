package collect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/finalize/mimawari/internal/config"
	"github.com/finalize/mimawari/internal/gh"
	"github.com/finalize/mimawari/internal/model"
	"github.com/finalize/mimawari/internal/npmreg"
	"github.com/finalize/mimawari/internal/site"
)

var fixedNow = time.Date(2026, 8, 27, 22, 0, 0, 0, time.UTC)

// fakeGitHub は termpic だけ「チェックが落ちている PR」を持ち、
// hidori は静かなリポジトリとして振る舞う。
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/finalize/termpic/pulls", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"number":1,"title":"deps: bump bumpp","html_url":"https://x/pull/1",
			"created_at":"2026-08-23T00:03:16Z","updated_at":"2026-08-23T00:03:16Z",
			"draft":false,"user":{"login":"dependabot[bot]"},"head":{"sha":"abc"}}]`)
	})
	mux.HandleFunc("GET /repos/finalize/termpic/pulls/{n}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"number":1,"mergeable_state":"blocked"}`)
	})
	mux.HandleFunc("GET /repos/finalize/termpic/commits/{sha}/check-runs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"check_runs":[{"name":"check & test & build","status":"completed","conclusion":"failure"}]}`)
	})
	mux.HandleFunc("GET /repos/{o}/{r}/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"workflow_runs":[]}`)
	})
	mux.HandleFunc("GET /repos/{o}/{r}/pulls", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	mux.HandleFunc("GET /repos/{o}/{r}/issues", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	mux.HandleFunc("GET /repos/{o}/{r}/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeNPM(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{pkg}/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"0.2.1"}`)
	})
	mux.HandleFunc("GET /downloads/point/last-week/{pkg}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"downloads":775}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRun(t *testing.T) {
	ghSrv, npmSrv := fakeGitHub(t), fakeNPM(t)
	siteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer siteSrv.Close()

	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "termpic"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(ws, "termpic", "package.json")
	if err := os.WriteFile(manifest, []byte(`{"version":"0.2.2"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Collector{
		Cfg: config.Config{
			Workspace:  ws,
			Thresholds: config.Thresholds{StalePRHours: 24, StaleIssueDays: 14, SlowSiteMS: 1500},
			Projects: []config.Project{
				{Name: "termpic", Repo: "finalize/termpic", Branch: "main",
					Site: siteSrv.URL, NPM: "termpic", Manifest: "termpic/package.json"},
				{Name: "hidori", Repo: "finalize/hidori", Branch: "main", Site: siteSrv.URL},
			},
		},
		GitHub: &gh.Client{BaseURL: ghSrv.URL, HTTP: ghSrv.Client()},
		NPM:    &npmreg.Client{Registry: npmSrv.URL, DownloadsAPI: npmSrv.URL, HTTP: npmSrv.Client()},
		Site:   site.New(siteSrv.Client()),
		Now:    func() time.Time { return fixedNow },
	}

	snap := c.Run(context.Background())

	if len(snap.Errors) != 0 {
		t.Fatalf("エラーが出ています: %+v", snap.Errors)
	}
	// 設定に並べた順を保つ。goroutine の終わった順にならないこと。
	if snap.Projects[0].Name != "termpic" || snap.Projects[1].Name != "hidori" {
		t.Fatalf("並び順 = %q, %q", snap.Projects[0].Name, snap.Projects[1].Name)
	}
	if !snap.GeneratedAt.Equal(fixedNow) {
		t.Errorf("GeneratedAt = %v", snap.GeneratedAt)
	}

	tp := snap.Projects[0]
	if len(tp.Pulls) != 1 || tp.Pulls[0].State != model.PullChecksFailing {
		t.Errorf("termpic の PR = %+v", tp.Pulls)
	}
	if tp.NPM == nil || !tp.NPM.Drift {
		t.Errorf("npm の差分が拾えていません: %+v", tp.NPM)
	}
	if tp.Site == nil || !tp.Site.OK {
		t.Errorf("サイト = %+v", tp.Site)
	}

	hd := snap.Projects[1]
	if hd.NPM != nil {
		t.Errorf("npm を設定していないのに %+v", hd.NPM)
	}

	// 見つかったことが Attention まで届いている。
	var kinds []string
	for _, it := range snap.Attention {
		kinds = append(kinds, it.Kind)
	}
	got := strings.Join(kinds, ",")
	if !strings.Contains(got, "pr-checks-failing") || !strings.Contains(got, "npm-drift") {
		t.Errorf("attention = %v", kinds)
	}
	if !snap.HasAction() {
		t.Error("HasAction が false です")
	}
}

// 情報源が落ちても Snapshot は返る。
func TestRunRecordsErrors(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer down.Close()

	c := &Collector{
		Cfg: config.Config{
			Thresholds: config.Thresholds{StalePRHours: 24, StaleIssueDays: 14, SlowSiteMS: 1500},
			Projects:   []config.Project{{Name: "x", Repo: "o/x", Branch: "main", Site: down.URL}},
		},
		GitHub: &gh.Client{BaseURL: down.URL, HTTP: down.Client()},
		Site:   site.New(down.Client()),
		Now:    func() time.Time { return fixedNow },
	}
	snap := c.Run(context.Background())

	if len(snap.Projects) != 1 {
		t.Fatalf("プロジェクトが返っていません: %+v", snap.Projects)
	}
	if len(snap.Errors) == 0 {
		t.Fatal("エラーが記録されていません")
	}
	// 取れなくても JSON が null にならないこと。
	if snap.Projects[0].Pulls == nil || snap.Projects[0].Issues == nil {
		t.Errorf("nil ではなく空スライスを返すこと: %+v", snap.Projects[0])
	}
	for _, e := range snap.Errors {
		if !strings.HasPrefix(e.Source, "github:") {
			t.Errorf("Source = %q", e.Source)
		}
	}
	// サイトが 500 を返したことは、エラーではなく結果として出る。
	if snap.Projects[0].Site == nil || snap.Projects[0].Site.OK {
		t.Errorf("サイトの状態 = %+v", snap.Projects[0].Site)
	}
	if !snap.HasAction() {
		t.Error("落ちているサイトが action になっていません")
	}
}

// 情報源を差していないときは、その系統を黙って飛ばす。
func TestRunWithoutClients(t *testing.T) {
	c := &Collector{
		Cfg: config.Config{
			Thresholds: config.Thresholds{StalePRHours: 24, StaleIssueDays: 14, SlowSiteMS: 1500},
			Projects:   []config.Project{{Name: "x", Repo: "o/x", Branch: "main"}},
		},
		Now: func() time.Time { return fixedNow },
	}
	snap := c.Run(context.Background())
	if len(snap.Errors) != 0 || len(snap.Attention) != 0 {
		t.Errorf("何も無いはず: %+v %+v", snap.Errors, snap.Attention)
	}
}
