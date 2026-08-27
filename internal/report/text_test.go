package report

import (
	"strings"
	"testing"

	"github.com/finalize/mimawari/internal/model"
)

func sample() model.Snapshot {
	return model.Snapshot{
		GeneratedAt: now,
		TookMS:      1240,
		Projects: []model.Project{
			{Name: "shogo-site", Repo: "finalize/portfolio", Site: &model.Site{URL: "https://a", OK: true, Status: 200, LatencyMS: 110}},
			{Name: "termpic", Repo: "finalize/termpic",
				Site:  &model.Site{URL: "https://b", OK: true, Status: 200, LatencyMS: 97},
				Pulls: []model.PullRequest{{Number: 1}},
				NPM:   &model.NPM{Package: "termpic", Published: "0.2.2", DownloadsLastWeek: 775}},
			{Name: "hidori", Repo: "finalize/hidori", Site: &model.Site{URL: "https://c", OK: false, Status: 502}},
		},
		Attention: []model.Item{
			{Project: "termpic", Kind: "pr-checks-failing", Severity: model.SevAction,
				Summary: "PR #1 のチェックが 1 件落ちている（4日）", Detail: "deps: bump bumpp", URL: "https://x/pull/1"},
			{Project: "hidori", Kind: "site-down", Severity: model.SevAction, Summary: "サイトが応答していない"},
		},
		Errors: []model.SourceError{{Source: "npm:termpic", Message: "429"}},
	}
}

func TestTextPlain(t *testing.T) {
	out := Text(sample(), false)
	if strings.Contains(out, "\x1b[") {
		t.Error("color=false なのにエスケープ列が入っています")
	}
	for _, want := range []string{
		"mimawari", "1.2s", "4日", "手を動かすところ (2)",
		"termpic", "deps: bump bumpp", "https://x/pull/1",
		"npm 0.2.2 週775", "site 200  110ms", "site 502",
		"取れなかったもの (1)", "npm:termpic",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出力にありません\n---\n%s", want, out)
		}
	}
}

// 色を付けても桁がずれない（詰めてから色を付けているか）。
func TestTextColumnsAlign(t *testing.T) {
	for _, color := range []bool{false, true} {
		out := Text(sample(), color)
		var widths []int
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, "issue ") {
				continue
			}
			widths = append(widths, strings.Index(stripANSI(line), "issue "))
		}
		if len(widths) < 2 {
			t.Fatalf("プロジェクト行が足りません:\n%s", out)
		}
		for _, w := range widths[1:] {
			if w != widths[0] {
				t.Errorf("color=%v で桁がずれています: %v\n%s", color, widths, out)
				break
			}
		}
	}
}

func TestTextNoAttention(t *testing.T) {
	s := sample()
	s.Attention = nil
	s.Errors = nil
	out := Text(s, false)
	if !strings.Contains(out, "手を動かすところはありません") {
		t.Errorf("何も無いときの一言がありません:\n%s", out)
	}
	if strings.Contains(out, "取れなかったもの") {
		t.Error("エラーが無いのに節が出ています")
	}
}

// -quiet の出力。パイプに流す前提なので、タブ区切りが崩れないこと。
func TestAttentionText(t *testing.T) {
	out := AttentionText(sample(), false)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("行数 = %d: %q", len(lines), out)
	}
	cols := strings.Split(lines[0], "\t")
	if len(cols) != 3 {
		t.Fatalf("列数 = %d: %q", len(cols), lines[0])
	}
	if strings.TrimSpace(cols[0]) != "action" || cols[1] != "termpic" {
		t.Errorf("1行目 = %q", lines[0])
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color=false なのにエスケープ列が入っています")
	}

	// 何も無ければ何も出さない（|| で繋いだときに空行を出さない）。
	empty := sample()
	empty.Attention = nil
	if got := AttentionText(empty, false); got != "" {
		t.Errorf("空のはず: %q", got)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
