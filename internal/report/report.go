// Package report は集めた事実を「今、手を動かすべきこと」に翻訳する。
//
// 生の状態をそのまま並べても見回りにはならない。止まっている理由ごとに
// 直し方が違うので、そこまで言い切ったものだけを Attention に載せる。
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/finalize/mimawari/internal/config"
	"github.com/finalize/mimawari/internal/model"
)

// Attention は注目すべき項目を、急ぐ順に並べて返す。
func Attention(projects []model.Project, th config.Thresholds, now time.Time) []model.Item {
	items := []model.Item{}
	for _, p := range projects {
		items = append(items, pullItems(p, th, now)...)
		items = append(items, npmItems(p)...)
		items = append(items, siteItems(p, th)...)
		items = append(items, issueItems(p, th, now)...)
		items = append(items, releaseItems(p)...)
	}
	// 同じ重さのものは設定に並べた順（＝プロジェクト順）のままにする。
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Severity.Rank() < items[j].Severity.Rank()
	})
	return items
}

func pullItems(p model.Project, th config.Thresholds, now time.Time) []model.Item {
	var out []model.Item
	for _, pr := range p.Pulls {
		age := pr.Age(now)
		since := pr.CreatedAt
		item := model.Item{
			Project: p.Name,
			URL:     pr.URL,
			Detail:  pr.Title,
			Since:   &since,
		}
		switch pr.State {
		case model.PullDraft:
			continue

		case model.PullNeedsApproval:
			item.Kind, item.Severity = "pr-needs-approval", model.SevAction
			item.Summary = fmt.Sprintf("PR #%d がワークフローの承認待ちで止まっている（%s）", pr.Number, Duration(age))
			item.Detail = withNames(pr.Title, "承認待ち", pr.Checks.ActionRequiredNames)

		case model.PullChecksFailing:
			item.Kind, item.Severity = "pr-checks-failing", model.SevAction
			item.Summary = fmt.Sprintf("PR #%d のチェックが %d 件落ちている（%s）", pr.Number, pr.Checks.Failure, Duration(age))
			item.Detail = withNames(pr.Title, "落ちている", pr.Checks.FailingNames)

		case model.PullConflict:
			item.Kind, item.Severity = "pr-conflict", model.SevAction
			item.Summary = fmt.Sprintf("PR #%d が競合している（%s）", pr.Number, Duration(age))

		case model.PullBlocked:
			item.Kind, item.Severity = "pr-blocked", model.SevAction
			item.Summary = fmt.Sprintf("PR #%d がブランチ保護で止まっている（%s）", pr.Number, Duration(age))
			item.Detail = pr.Title + " — チェックは落ちておらず待ってもいない。必須チェックが始まっていないか、レビューが足りていない"

		case model.PullBehind:
			item.Kind, item.Severity = "pr-behind", model.SevWarn
			item.Summary = fmt.Sprintf("PR #%d が base に追いついていない（%s）", pr.Number, Duration(age))

		case model.PullChecksPending:
			// 走っている最中なら待てばいい。長すぎるときだけ知らせる。
			if age < time.Duration(th.StalePRHours)*time.Hour {
				continue
			}
			item.Kind, item.Severity = "pr-checks-stuck", model.SevWarn
			item.Summary = fmt.Sprintf("PR #%d のチェックが %s 終わっていない", pr.Number, Duration(age))

		case model.PullMergeable:
			if pr.AutoMerge {
				// 自動マージが有効なら放っておけば入る。
				continue
			}
			item.Kind, item.Severity = "pr-mergeable", model.SevWarn
			item.Summary = fmt.Sprintf("PR #%d はマージできる（%s）", pr.Number, Duration(age))

		default:
			continue
		}
		out = append(out, item)
	}
	return out
}

func npmItems(p model.Project) []model.Item {
	if p.NPM == nil || !p.NPM.Drift {
		return nil
	}
	return []model.Item{{
		Project:  p.Name,
		Kind:     "npm-drift",
		Severity: model.SevAction,
		Summary: fmt.Sprintf("%s の手元は %s だが npm は %s のまま",
			p.NPM.Package, p.NPM.Local, p.NPM.Published),
		Detail: "publish し忘れか、公開後に上げ忘れている",
		URL:    p.NPM.URL,
	}}
}

func siteItems(p model.Project, th config.Thresholds) []model.Item {
	s := p.Site
	if s == nil {
		return nil
	}
	if !s.OK {
		detail := s.Error
		if detail == "" {
			detail = fmt.Sprintf("状態コード %d", s.Status)
		}
		return []model.Item{{
			Project:  p.Name,
			Kind:     "site-down",
			Severity: model.SevAction,
			Summary:  "サイトが応答していない",
			Detail:   detail,
			URL:      s.URL,
		}}
	}
	if th.SlowSiteMS > 0 && s.LatencyMS >= int64(th.SlowSiteMS) {
		return []model.Item{{
			Project:  p.Name,
			Kind:     "site-slow",
			Severity: model.SevWarn,
			Summary:  fmt.Sprintf("サイトの応答が %dms かかっている", s.LatencyMS),
			URL:      s.URL,
		}}
	}
	return nil
}

func issueItems(p model.Project, th config.Thresholds, now time.Time) []model.Item {
	var out []model.Item
	limit := time.Duration(th.StaleIssueDays) * 24 * time.Hour
	for _, is := range p.Issues {
		idle := now.Sub(is.UpdatedAt)
		if idle < limit {
			continue
		}
		since := is.UpdatedAt
		out = append(out, model.Item{
			Project:  p.Name,
			Kind:     "issue-stale",
			Severity: model.SevInfo,
			Summary:  fmt.Sprintf("issue #%d が %s 動いていない", is.Number, Duration(idle)),
			Detail:   is.Title,
			URL:      is.URL,
			Since:    &since,
		})
	}
	return out
}

func releaseItems(p model.Project) []model.Item {
	r := p.Release
	if r == nil || r.CommitsSince <= 0 {
		return nil
	}
	return []model.Item{{
		Project:  p.Name,
		Kind:     "release-behind",
		Severity: model.SevInfo,
		Summary:  fmt.Sprintf("%s から %d コミット進んでいる", r.Tag, r.CommitsSince),
		URL:      r.URL,
	}}
}

// withNames は本文にチェック名を添える。名前が無ければ本文だけ返す。
func withNames(title, label string, names []string) string {
	if len(names) == 0 {
		return title
	}
	return fmt.Sprintf("%s — %s: %s", title, label, strings.Join(names, ", "))
}

// Duration は経過時間をざっくり日本語にする。
// 見回りで欲しいのは桁であって、秒単位の正確さではない。
func Duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "たった今"
	case d < time.Hour:
		return fmt.Sprintf("%d分", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d時間", int(d.Hours()))
	default:
		return fmt.Sprintf("%d日", int(d.Hours()/24))
	}
}
