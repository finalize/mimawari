package gh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/finalize/mimawari/internal/model"
)

// Classify がこの API の肝。mergeable_state だけでは「承認待ち」と
// 「CI が落ちている」を取り違えるので、組み合わせを表で固定する。
func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		draft bool
		state string
		ch    model.ChecksSummary
		want  model.PullState
	}{
		{"下書きは何より先", true, "blocked", model.ChecksSummary{Failure: 1}, model.PullDraft},
		{"承認待ちは落ちているより先", false, "blocked", model.ChecksSummary{ActionRequired: 1, Failure: 1}, model.PullNeedsApproval},
		{"競合", false, "dirty", model.ChecksSummary{}, model.PullConflict},
		{"競合は落ちているより先", false, "dirty", model.ChecksSummary{Failure: 1}, model.PullConflict},
		{"チェックが落ちている", false, "blocked", model.ChecksSummary{Failure: 1, Success: 2}, model.PullChecksFailing},
		{"unstable でも落ちていれば落ちている", false, "unstable", model.ChecksSummary{Failure: 1}, model.PullChecksFailing},
		{"base に遅れている", false, "behind", model.ChecksSummary{Success: 1}, model.PullBehind},
		{"チェック待ち", false, "blocked", model.ChecksSummary{Pending: 1}, model.PullChecksPending},
		{"マージできる", false, "clean", model.ChecksSummary{Success: 3}, model.PullMergeable},
		{"unstable でも落ちていなければマージできる", false, "unstable", model.ChecksSummary{Success: 1}, model.PullMergeable},
		{"説明が付かない blocked", false, "blocked", model.ChecksSummary{Success: 2}, model.PullBlocked},
		{"チェックが1つも無い blocked", false, "blocked", model.ChecksSummary{}, model.PullBlocked},
		{"計算中", false, "unknown", model.ChecksSummary{}, model.PullUnknown},
		{"状態が取れなかった", false, "", model.ChecksSummary{}, model.PullUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.draft, tt.state, tt.ch); got != tt.want {
				t.Errorf("Classify(%v, %q, %+v) = %q, 期待は %q", tt.draft, tt.state, tt.ch, got, tt.want)
			}
		})
	}
}

// fakeGitHub は必要なエンドポイントだけを返す偽の GitHub。
type fakeGitHub struct {
	pulls      string
	pullDetail string
	checkRuns  string
	runs       string
	issues     string
	release    string
	compare    string
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	add := func(pattern, body string, code int) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		})
	}
	add("GET /repos/o/r/pulls", f.pulls, 200)
	add("GET /repos/o/r/pulls/{n}", f.pullDetail, 200)
	add("GET /repos/o/r/commits/{sha}/check-runs", f.checkRuns, 200)
	add("GET /repos/o/r/actions/runs", f.runs, 200)
	add("GET /repos/o/r/issues", f.issues, 200)
	if f.release == "" {
		add("GET /repos/o/r/releases/latest", `{"message":"Not Found"}`, 404)
	} else {
		add("GET /repos/o/r/releases/latest", f.release, 200)
	}
	add("GET /repos/o/r/compare/{rest...}", f.compare, 200)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetch(t *testing.T) {
	f := &fakeGitHub{
		pulls: `[{"number":1,"title":"deps: bump bumpp","html_url":"https://x/pull/1",
			"created_at":"2026-08-23T00:03:16Z","updated_at":"2026-08-23T00:03:16Z",
			"draft":false,"user":{"login":"dependabot[bot]"},"head":{"sha":"abc"},"auto_merge":null}]`,
		pullDetail: `{"number":1,"mergeable_state":"blocked","auto_merge":{}}`,
		checkRuns: `{"check_runs":[
			{"name":"check & test & build","status":"completed","conclusion":"failure"},
			{"name":"Claude Review","status":"completed","conclusion":"success"},
			{"name":"deploy","status":"completed","conclusion":"skipped"},
			{"name":"e2e","status":"in_progress","conclusion":null}]}`,
		runs:    `{"workflow_runs":[{"name":"CI","status":"completed","conclusion":"failure"}]}`,
		issues:  `[{"number":3,"title":"週次点検","html_url":"https://x/issues/3","created_at":"2026-08-22T00:00:00Z","updated_at":"2026-08-22T00:00:00Z","labels":[{"name":"maintenance"}]},{"number":9,"title":"これは PR","html_url":"https://x/pull/9","created_at":"2026-08-22T00:00:00Z","updated_at":"2026-08-22T00:00:00Z","pull_request":{}}]`,
		release: `{"tag_name":"v0.2.2","html_url":"https://x/releases/v0.2.2","published_at":"2026-08-20T00:00:00Z"}`,
		compare: `{"ahead_by":3}`,
	}
	srv := f.server(t)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), MaxPulls: 20}

	repo, errs := c.Fetch(context.Background(), "o/r", "main")
	if len(errs) != 0 {
		t.Fatalf("エラーが出ています: %v", errs)
	}

	if len(repo.Pulls) != 1 {
		t.Fatalf("PR 数 = %d, 期待は 1", len(repo.Pulls))
	}
	pr := repo.Pulls[0]
	if pr.State != model.PullChecksFailing {
		t.Errorf("State = %q, 期待は %q", pr.State, model.PullChecksFailing)
	}
	if pr.Checks.Failure != 1 || pr.Checks.Success != 1 || pr.Checks.Skipped != 1 || pr.Checks.Pending != 1 {
		t.Errorf("チェックの内訳が合いません: %+v", pr.Checks)
	}
	if len(pr.Checks.FailingNames) != 1 || pr.Checks.FailingNames[0] != "check & test & build" {
		t.Errorf("落ちたチェック名 = %v", pr.Checks.FailingNames)
	}
	if !pr.AutoMerge {
		t.Error("詳細側の auto_merge が拾えていません")
	}
	if pr.Author != "dependabot[bot]" {
		t.Errorf("Author = %q", pr.Author)
	}

	// issues エンドポイントは PR も返すので落とす。
	if len(repo.Issues) != 1 || repo.Issues[0].Number != 3 {
		t.Fatalf("issue = %+v, PR を除いた1件のはず", repo.Issues)
	}
	if len(repo.Issues[0].Labels) != 1 || repo.Issues[0].Labels[0] != "maintenance" {
		t.Errorf("ラベル = %v", repo.Issues[0].Labels)
	}

	if repo.Release == nil || repo.Release.Tag != "v0.2.2" || repo.Release.CommitsSince != 3 {
		t.Errorf("リリース = %+v", repo.Release)
	}
}

func TestFetchApprovalFromWorkflowRuns(t *testing.T) {
	// チェック一覧には承認待ちが現れず、ワークフロー側にだけ出る場合。
	// これが実際に困った形（必須チェックが承認待ちのまま PR が blocked になる）。
	f := &fakeGitHub{
		pulls: `[{"number":2,"title":"日次点検","html_url":"https://x/pull/2",
			"created_at":"2026-08-26T00:00:00Z","updated_at":"2026-08-26T00:00:00Z",
			"draft":false,"user":{"login":"claude"},"head":{"sha":"def"},"auto_merge":null}]`,
		pullDetail: `{"number":2,"mergeable_state":"blocked"}`,
		checkRuns:  `{"check_runs":[]}`,
		runs: `{"workflow_runs":[
			{"name":"CI","status":"action_required","conclusion":null},
			{"name":"Claude Review","status":"action_required","conclusion":null},
			{"name":"deploy","status":"completed","conclusion":"success"}]}`,
		issues:  `[]`,
		compare: `{"ahead_by":0}`,
	}
	srv := f.server(t)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}

	repo, errs := c.Fetch(context.Background(), "o/r", "main")
	if len(errs) != 0 {
		t.Fatalf("エラーが出ています: %v", errs)
	}
	pr := repo.Pulls[0]
	if pr.State != model.PullNeedsApproval {
		t.Errorf("State = %q, 期待は %q（承認待ちは blocked と区別する）", pr.State, model.PullNeedsApproval)
	}
	if pr.Checks.ActionRequired != 2 {
		t.Errorf("ActionRequired = %d, 期待は 2", pr.Checks.ActionRequired)
	}
	if strings.Join(pr.Checks.ActionRequiredNames, ",") != "CI,Claude Review" {
		t.Errorf("承認待ちの名前 = %v", pr.Checks.ActionRequiredNames)
	}
	if repo.Release != nil {
		t.Errorf("リリースが無いリポジトリは nil のはず: %+v", repo.Release)
	}
}

func TestFetchPartialFailure(t *testing.T) {
	// issue の取得だけ失敗しても、PR は返る。
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"message":"boom"}`)
	})
	mux.HandleFunc("GET /repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	repo, errs := c.Fetch(context.Background(), "o/r", "main")
	if len(errs) != 1 {
		t.Fatalf("エラー数 = %d, 期待は 1: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "issue") {
		t.Errorf("エラー = %v, issue のものであるはず", errs[0])
	}
	if repo.Pulls == nil && repo.Issues != nil {
		t.Error("PR 側まで巻き込まれています")
	}
}

func TestAuthorizationHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "t0ken", HTTP: srv.Client()}
	_, _ = c.issues(context.Background(), "o/r")
	if got != "Bearer t0ken" {
		t.Errorf("Authorization = %q", got)
	}

	c.Token = ""
	_, _ = c.issues(context.Background(), "o/r")
	if got != "" {
		t.Errorf("トークン未設定なのに Authorization = %q", got)
	}
}
