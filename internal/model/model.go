// Package model は見回りの結果を表す型をまとめる。
// 収集側（gh / npmreg / site）と、組み立て側（collect）、出力側（report / server）が
// 共通で使う語彙をここに置いて、依存の向きを一方通行にしている。
package model

import "time"

// Severity は「どれくらい急いで手を動かすべきか」を表す。
type Severity string

const (
	// SevAction は人が操作しないと先に進まないもの。
	SevAction Severity = "action"
	// SevWarn は放っておくと困るが、まだ動いているもの。
	SevWarn Severity = "warn"
	// SevInfo は知っておくと良い程度のもの。
	SevInfo Severity = "info"
)

// Rank は並べ替え用の重み。小さいほど先に出す。
func (s Severity) Rank() int {
	switch s {
	case SevAction:
		return 0
	case SevWarn:
		return 1
	default:
		return 2
	}
}

// Item は「今、手を動かすべきこと」1件。
type Item struct {
	Project  string     `json:"project"`
	Kind     string     `json:"kind"`
	Severity Severity   `json:"severity"`
	Summary  string     `json:"summary"`
	Detail   string     `json:"detail,omitempty"`
	URL      string     `json:"url,omitempty"`
	Since    *time.Time `json:"since,omitempty"`
}

// PullState は PR が止まっている理由の分類。
//
// GitHub の mergeable_state は blocked としか言わないので、
// 「承認待ち」なのか「CI が落ちている」のかがそれだけでは分からない。
// この API を作った動機がまさにそこなので、チェックの内訳まで見て分類する。
type PullState string

const (
	// PullNeedsApproval はワークフローが承認待ち（action_required）で止まっている状態。
	PullNeedsApproval PullState = "needs-approval"
	// PullChecksFailing はチェックが落ちている状態。
	PullChecksFailing PullState = "checks-failing"
	// PullChecksPending はチェックの完了待ち。
	PullChecksPending PullState = "checks-pending"
	// PullConflict は競合していて、そのままではマージできない状態。
	PullConflict PullState = "conflict"
	// PullBehind は base に追いついていない状態。
	PullBehind PullState = "behind"
	// PullBlocked は落ちても待ってもいないのにブランチ保護で止まっている状態。
	// 必須チェックが始まっていないか、レビューが足りていない。
	PullBlocked PullState = "blocked"
	// PullMergeable はマージできる状態。
	PullMergeable PullState = "mergeable"
	// PullDraft は下書き。
	PullDraft PullState = "draft"
	// PullUnknown は判定できなかった状態。
	PullUnknown PullState = "unknown"
)

// ChecksSummary は PR の head コミットに紐づくチェックの内訳。
//
// 数だけでなく落ちたチェックの名前も持つ。「1件落ちている」までしか分からないと
// 結局リポジトリを開くことになり、見回りの意味が半分無くなる。
type ChecksSummary struct {
	Total          int `json:"total"`
	Success        int `json:"success"`
	Failure        int `json:"failure"`
	Pending        int `json:"pending"`
	ActionRequired int `json:"action_required"`
	Skipped        int `json:"skipped"`

	FailingNames        []string `json:"failing_names,omitempty"`
	ActionRequiredNames []string `json:"action_required_names,omitempty"`
}

// PullRequest は open な PR 1件。
type PullRequest struct {
	Number         int           `json:"number"`
	Title          string        `json:"title"`
	Author         string        `json:"author"`
	URL            string        `json:"url"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	Draft          bool          `json:"draft"`
	MergeableState string        `json:"mergeable_state"`
	AutoMerge      bool          `json:"auto_merge"`
	State          PullState     `json:"state"`
	Checks         ChecksSummary `json:"checks"`
}

// Age は PR が開いてからの経過時間を返す。
func (p PullRequest) Age(now time.Time) time.Duration { return now.Sub(p.CreatedAt) }

// Issue は open な issue 1件。
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Labels    []string  `json:"labels,omitempty"`
}

// Release は最新のリリースと、そこからの進み具合。
type Release struct {
	Tag          string    `json:"tag"`
	PublishedAt  time.Time `json:"published_at"`
	URL          string    `json:"url"`
	CommitsSince int       `json:"commits_since"`
}

// NPM は npm に公開しているパッケージの状態。
type NPM struct {
	Package           string `json:"package"`
	Published         string `json:"published"`
	Local             string `json:"local,omitempty"`
	Drift             bool   `json:"drift"`
	DownloadsLastWeek int    `json:"downloads_last_week"`
	URL               string `json:"url"`
}

// Site は公開しているサイトの死活。
type Site struct {
	URL       string `json:"url"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

// Project は1プロジェクト分の見回り結果。
type Project struct {
	Name    string        `json:"name"`
	Repo    string        `json:"repo"`
	Pulls   []PullRequest `json:"pulls"`
	Issues  []Issue       `json:"issues"`
	Release *Release      `json:"release,omitempty"`
	NPM     *NPM          `json:"npm,omitempty"`
	Site    *Site         `json:"site,omitempty"`
}

// SourceError は取得に失敗した情報源。
//
// 1つの情報源が落ちても他は返す。集約する API が全部まとめて失敗すると、
// 見回りとしては使い物にならないため、失敗もデータとして返している。
type SourceError struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

// Snapshot は1回の見回りの全結果。
type Snapshot struct {
	GeneratedAt time.Time     `json:"generated_at"`
	TookMS      int64         `json:"took_ms"`
	Attention   []Item        `json:"attention"`
	Projects    []Project     `json:"projects"`
	Errors      []SourceError `json:"errors,omitempty"`
}

// HasAction は人の操作待ちの項目が1件でもあるかを返す。
func (s Snapshot) HasAction() bool {
	for _, it := range s.Attention {
		if it.Severity == SevAction {
			return true
		}
	}
	return false
}
