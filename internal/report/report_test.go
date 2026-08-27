package report

import (
	"strings"
	"testing"
	"time"

	"github.com/finalize/mimawari/internal/config"
	"github.com/finalize/mimawari/internal/model"
)

var (
	now = time.Date(2026, 8, 27, 22, 0, 0, 0, time.UTC)
	th  = config.Thresholds{StalePRHours: 24, StaleIssueDays: 14, SlowSiteMS: 1500}
)

func ago(d time.Duration) time.Time { return now.Add(-d) }

func pr(state model.PullState, age time.Duration, mut ...func(*model.PullRequest)) model.PullRequest {
	p := model.PullRequest{Number: 1, Title: "題名", State: state, CreatedAt: ago(age), URL: "https://x/pull/1"}
	for _, f := range mut {
		f(&p)
	}
	return p
}

func kinds(items []model.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Kind
	}
	return out
}

func TestAttentionPulls(t *testing.T) {
	tests := []struct {
		name     string
		pull     model.PullRequest
		wantKind string
		wantSev  model.Severity
	}{
		{"承認待ち", pr(model.PullNeedsApproval, 4*24*time.Hour), "pr-needs-approval", model.SevAction},
		{"チェックが落ちている", pr(model.PullChecksFailing, 4*24*time.Hour, func(p *model.PullRequest) {
			p.Checks = model.ChecksSummary{Failure: 1, FailingNames: []string{"check & test & build"}}
		}), "pr-checks-failing", model.SevAction},
		{"競合", pr(model.PullConflict, time.Hour), "pr-conflict", model.SevAction},
		{"説明が付かない blocked", pr(model.PullBlocked, time.Hour), "pr-blocked", model.SevAction},
		{"base に遅れている", pr(model.PullBehind, time.Hour), "pr-behind", model.SevWarn},
		{"長く終わらないチェック", pr(model.PullChecksPending, 30*time.Hour), "pr-checks-stuck", model.SevWarn},
		{"マージできるのに自動マージが無い", pr(model.PullMergeable, time.Hour), "pr-mergeable", model.SevWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Attention([]model.Project{{Name: "p", Pulls: []model.PullRequest{tt.pull}}}, th, now)
			if len(got) != 1 {
				t.Fatalf("項目数 = %d (%v)", len(got), kinds(got))
			}
			if got[0].Kind != tt.wantKind || got[0].Severity != tt.wantSev {
				t.Errorf("kind = %q / sev = %q, 期待は %q / %q", got[0].Kind, got[0].Severity, tt.wantKind, tt.wantSev)
			}
		})
	}
}

func TestAttentionPullsSkipped(t *testing.T) {
	tests := []struct {
		name string
		pull model.PullRequest
	}{
		{"下書きは知らせない", pr(model.PullDraft, 10*24*time.Hour)},
		{"自動マージ待ちは放っておけば入る", pr(model.PullMergeable, time.Hour, func(p *model.PullRequest) { p.AutoMerge = true })},
		{"走り始めたばかりのチェックは待てばいい", pr(model.PullChecksPending, time.Hour)},
		{"判定できないものは黙る", pr(model.PullUnknown, time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Attention([]model.Project{{Name: "p", Pulls: []model.PullRequest{tt.pull}}}, th, now)
			if len(got) != 0 {
				t.Errorf("知らせないはずが %v", kinds(got))
			}
		})
	}
}

// 落ちたチェックの名前を本文に出す。ここが無いと結局リポジトリを開くことになる。
func TestAttentionShowsCheckNames(t *testing.T) {
	p := pr(model.PullChecksFailing, time.Hour, func(p *model.PullRequest) {
		p.Checks = model.ChecksSummary{Failure: 1, FailingNames: []string{"check & test & build"}}
	})
	got := Attention([]model.Project{{Name: "termpic", Pulls: []model.PullRequest{p}}}, th, now)
	if !strings.Contains(got[0].Detail, "check & test & build") {
		t.Errorf("Detail = %q", got[0].Detail)
	}
}

func TestAttentionNPMDrift(t *testing.T) {
	base := model.Project{Name: "termpic"}

	base.NPM = &model.NPM{Package: "termpic", Published: "0.2.2", Local: "0.2.2"}
	if got := Attention([]model.Project{base}, th, now); len(got) != 0 {
		t.Errorf("一致しているのに %v", kinds(got))
	}

	base.NPM = &model.NPM{Package: "termpic", Published: "0.2.1", Local: "0.2.2", Drift: true}
	got := Attention([]model.Project{base}, th, now)
	if len(got) != 1 || got[0].Kind != "npm-drift" || got[0].Severity != model.SevAction {
		t.Fatalf("項目 = %+v", got)
	}
	if !strings.Contains(got[0].Summary, "0.2.2") || !strings.Contains(got[0].Summary, "0.2.1") {
		t.Errorf("両方のバージョンを出すはず: %q", got[0].Summary)
	}
}

func TestAttentionSite(t *testing.T) {
	tests := []struct {
		name     string
		site     model.Site
		wantKind string
	}{
		{"生きていて速い", model.Site{OK: true, Status: 200, LatencyMS: 120}, ""},
		{"落ちている", model.Site{OK: false, Status: 502}, "site-down"},
		{"届かない", model.Site{OK: false, Error: "dial tcp: refused"}, "site-down"},
		{"遅い", model.Site{OK: true, Status: 200, LatencyMS: 1500}, "site-slow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.site
			got := Attention([]model.Project{{Name: "p", Site: &s}}, th, now)
			if tt.wantKind == "" {
				if len(got) != 0 {
					t.Fatalf("知らせないはずが %v", kinds(got))
				}
				return
			}
			if len(got) != 1 || got[0].Kind != tt.wantKind {
				t.Fatalf("項目 = %v, 期待は %q", kinds(got), tt.wantKind)
			}
			if got[0].Detail == "" && tt.wantKind == "site-down" {
				t.Error("落ちている理由が空です")
			}
		})
	}
}

func TestAttentionIssuesAndRelease(t *testing.T) {
	p := model.Project{
		Name: "shogo-site",
		Issues: []model.Issue{
			{Number: 3, Title: "週次点検", UpdatedAt: ago(20 * 24 * time.Hour)},
			{Number: 21, Title: "日次点検", UpdatedAt: ago(2 * 24 * time.Hour)},
		},
		Release: &model.Release{Tag: "v1.0.0", CommitsSince: 7},
	}
	got := Attention([]model.Project{p}, th, now)
	if len(got) != 2 {
		t.Fatalf("項目 = %v, 期待は 2（滞留 issue 1件 + リリース差分）", kinds(got))
	}
	for _, it := range got {
		if it.Severity != model.SevInfo {
			t.Errorf("%s の重さ = %q, info のはず", it.Kind, it.Severity)
		}
	}
}

// 急ぐものから順に並ぶ。同じ重さならプロジェクトの並び順のまま。
func TestAttentionOrder(t *testing.T) {
	projects := []model.Project{
		{Name: "a", Issues: []model.Issue{{Number: 1, UpdatedAt: ago(30 * 24 * time.Hour)}}},
		{Name: "b", Pulls: []model.PullRequest{pr(model.PullMergeable, time.Hour)}},
		{Name: "c", Pulls: []model.PullRequest{pr(model.PullNeedsApproval, time.Hour)}},
		{Name: "d", Pulls: []model.PullRequest{pr(model.PullConflict, time.Hour)}},
	}
	got := Attention(projects, th, now)
	var order []string
	for _, it := range got {
		order = append(order, it.Project)
	}
	want := []string{"c", "d", "b", "a"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("並び = %v, 期待は %v", order, want)
	}
}

func TestAttentionEmpty(t *testing.T) {
	got := Attention([]model.Project{{Name: "p"}}, th, now)
	if got == nil {
		t.Error("空でも nil ではなく空スライスを返すこと（JSON が null になる）")
	}
	if len(got) != 0 {
		t.Errorf("項目 = %v", kinds(got))
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "たった今"},
		{5 * time.Minute, "5分"},
		{90 * time.Minute, "1時間"},
		{25 * time.Hour, "1日"},
		{4 * 24 * time.Hour, "4日"},
	}
	for _, tt := range tests {
		if got := Duration(tt.in); got != tt.want {
			t.Errorf("Duration(%v) = %q, 期待は %q", tt.in, got, tt.want)
		}
	}
}
