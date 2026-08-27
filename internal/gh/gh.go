// Package gh は GitHub の REST API から、止まっている PR と滞留している issue を取る。
//
// この API を作った動機は「PR が BLOCKED のまま何日も気づかれない」ことなので、
// mergeable_state をそのまま返すのではなく、チェックの内訳まで見て
// 「承認待ち」「CI が落ちている」「チェック待ち」を区別するところまでを担当する。
package gh

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/finalize/mimawari/internal/httpx"
	"github.com/finalize/mimawari/internal/model"
)

// DefaultBaseURL は github.com の API。
const DefaultBaseURL = "https://api.github.com"

// Client は GitHub の API を叩く。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	// MaxPulls は1リポジトリあたりで詳しく調べる PR の上限。
	MaxPulls int
}

// New は既定の設定の Client を返す。token は空でもよい（公開リポジトリのみ・低いレート上限）。
func New(token string, hc *http.Client) *Client {
	return &Client{BaseURL: DefaultBaseURL, Token: token, HTTP: hc, MaxPulls: 20}
}

func (c *Client) header() http.Header {
	h := http.Header{}
	h.Set("Accept", "application/vnd.github+json")
	h.Set("X-GitHub-Api-Version", "2022-11-28")
	h.Set("User-Agent", "mimawari")
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
	return h
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	return httpx.GetJSON(ctx, c.HTTP, base+path, c.header(), v)
}

// Repo は1リポジトリ分の GitHub 側の状態。
type Repo struct {
	Pulls   []model.PullRequest
	Issues  []model.Issue
	Release *model.Release
}

// Fetch は repo の open PR・open issue・最新リリースをまとめて取る。
//
// 3系統を並列に取り、PR はさらに1件ずつ並列で詳細とチェックを引く。
// 一部が失敗しても残りは返す。返る error は「取れなかったもの」の一覧。
func (c *Client) Fetch(ctx context.Context, repo, branch string) (Repo, []error) {
	var (
		mu   sync.Mutex
		out  Repo
		errs []error
	)
	fail := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		pulls, perrs := c.pulls(ctx, repo)
		mu.Lock()
		out.Pulls = pulls
		errs = append(errs, perrs...)
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		issues, err := c.issues(ctx, repo)
		if err != nil {
			fail(fmt.Errorf("%s の issue: %w", repo, err))
			return
		}
		mu.Lock()
		out.Issues = issues
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		rel, err := c.release(ctx, repo, branch)
		if err != nil {
			fail(fmt.Errorf("%s のリリース: %w", repo, err))
			return
		}
		mu.Lock()
		out.Release = rel
		mu.Unlock()
	}()

	wg.Wait()
	return out, errs
}

type rawPull struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Draft     bool      `json:"draft"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	AutoMerge      *struct{} `json:"auto_merge"`
	MergeableState string    `json:"mergeable_state"`
}

func (c *Client) pulls(ctx context.Context, repo string) ([]model.PullRequest, []error) {
	var list []rawPull
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls?state=open&per_page=%d", repo, c.limit()), &list); err != nil {
		return nil, []error{fmt.Errorf("%s の PR 一覧: %w", repo, err)}
	}
	if len(list) > c.limit() {
		list = list[:c.limit()]
	}

	var (
		mu   sync.Mutex
		errs []error
	)
	out := make([]model.PullRequest, len(list))
	var wg sync.WaitGroup
	for i, p := range list {
		wg.Add(1)
		go func(i int, p rawPull) {
			defer wg.Done()
			pr, perrs := c.enrich(ctx, repo, p)
			out[i] = pr
			if len(perrs) > 0 {
				mu.Lock()
				errs = append(errs, perrs...)
				mu.Unlock()
			}
		}(i, p)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, errs
}

// enrich は一覧では取れない mergeable_state とチェックの内訳を足す。
func (c *Client) enrich(ctx context.Context, repo string, p rawPull) (model.PullRequest, []error) {
	pr := model.PullRequest{
		Number:    p.Number,
		Title:     p.Title,
		Author:    p.User.Login,
		URL:       p.HTMLURL,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
		Draft:     p.Draft,
		AutoMerge: p.AutoMerge != nil,
	}

	var (
		mu    sync.Mutex
		errs  []error
		wg    sync.WaitGroup
		state string
		sum   model.ChecksSummary
		wfAR  []string
	)
	fail := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	wg.Add(3)
	go func() {
		defer wg.Done()
		var detail rawPull
		if err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, p.Number), &detail); err != nil {
			fail(fmt.Errorf("%s#%d の詳細: %w", repo, p.Number, err))
			return
		}
		mu.Lock()
		state = detail.MergeableState
		if detail.AutoMerge != nil {
			pr.AutoMerge = true
		}
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		s, err := c.checkRuns(ctx, repo, p.Head.SHA)
		if err != nil {
			fail(fmt.Errorf("%s#%d のチェック: %w", repo, p.Number, err))
			return
		}
		mu.Lock()
		sum = s
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		names, err := c.workflowsAwaitingApproval(ctx, repo, p.Head.SHA)
		if err != nil {
			fail(fmt.Errorf("%s#%d のワークフロー: %w", repo, p.Number, err))
			return
		}
		mu.Lock()
		wfAR = names
		mu.Unlock()
	}()
	wg.Wait()

	// 承認待ちはチェック一覧に出ないことがあるので、ワークフロー側の数で底上げする。
	if len(wfAR) > sum.ActionRequired {
		sum.ActionRequired = len(wfAR)
		sum.ActionRequiredNames = wfAR
	}
	pr.MergeableState = state
	pr.Checks = sum
	pr.State = Classify(pr.Draft, state, sum)
	return pr, errs
}

type rawCheckRuns struct {
	CheckRuns []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"check_runs"`
}

func (c *Client) checkRuns(ctx context.Context, repo, sha string) (model.ChecksSummary, error) {
	var raw rawCheckRuns
	if sha == "" {
		return model.ChecksSummary{}, nil
	}
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100", repo, url.PathEscape(sha)), &raw); err != nil {
		return model.ChecksSummary{}, err
	}
	var s model.ChecksSummary
	for _, r := range raw.CheckRuns {
		s.Total++
		switch {
		case r.Conclusion == "action_required":
			s.ActionRequired++
			s.ActionRequiredNames = append(s.ActionRequiredNames, r.Name)
		case r.Status != "completed":
			s.Pending++
		case r.Conclusion == "success" || r.Conclusion == "neutral":
			s.Success++
		case r.Conclusion == "skipped" || r.Conclusion == "cancelled":
			s.Skipped++
		case r.Conclusion == "":
			s.Pending++
		default: // failure / timed_out / stale
			s.Failure++
			s.FailingNames = append(s.FailingNames, r.Name)
		}
	}
	return s, nil
}

type rawWorkflowRuns struct {
	WorkflowRuns []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"workflow_runs"`
}

// workflowsAwaitingApproval は「人が承認ボタンを押すまで動かない」ワークフローの数を返す。
//
// ブランチ保護の必須チェックがこの状態のまま止まると、PR は blocked のままになるが
// CI が落ちているわけではない。両者を取り違えると直し方を間違えるので、別に数える。
func (c *Client) workflowsAwaitingApproval(ctx context.Context, repo, sha string) ([]string, error) {
	if sha == "" {
		return nil, nil
	}
	var raw rawWorkflowRuns
	path := fmt.Sprintf("/repos/%s/actions/runs?head_sha=%s&per_page=100", repo, url.QueryEscape(sha))
	if err := c.get(ctx, path, &raw); err != nil {
		if httpx.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, r := range raw.WorkflowRuns {
		if r.Status == "action_required" || r.Conclusion == "action_required" || r.Status == "waiting" {
			names = append(names, r.Name)
		}
	}
	return names, nil
}

// Classify は PR が止まっている理由を1つに決める。
//
// 先に見たものが勝つ。直し方が違うものほど先に置いている
// （承認は人が押す・競合は手元で直す・CI は原因を追う）。
func Classify(draft bool, mergeableState string, ch model.ChecksSummary) model.PullState {
	if draft {
		return model.PullDraft
	}
	if ch.ActionRequired > 0 {
		return model.PullNeedsApproval
	}
	if mergeableState == "dirty" {
		return model.PullConflict
	}
	if ch.Failure > 0 {
		return model.PullChecksFailing
	}
	if mergeableState == "behind" {
		return model.PullBehind
	}
	if ch.Pending > 0 {
		return model.PullChecksPending
	}
	switch mergeableState {
	case "clean", "has_hooks", "unstable":
		return model.PullMergeable
	case "blocked":
		// 落ちても待ってもいないのに blocked。必須チェックが始まっていないか、
		// レビューが足りていない。どちらも人が動かないと進まない。
		return model.PullBlocked
	case "draft":
		return model.PullDraft
	}
	return model.PullUnknown
}

type rawIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HTMLURL     string    `json:"html_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PullRequest *struct{} `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (c *Client) issues(ctx context.Context, repo string) ([]model.Issue, error) {
	var list []rawIssue
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/issues?state=open&per_page=%d", repo, c.limit()), &list); err != nil {
		return nil, err
	}
	out := make([]model.Issue, 0, len(list))
	for _, r := range list {
		// issues エンドポイントは PR も返すので落とす。
		if r.PullRequest != nil {
			continue
		}
		is := model.Issue{
			Number:    r.Number,
			Title:     r.Title,
			URL:       r.HTMLURL,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
		for _, l := range r.Labels {
			is.Labels = append(is.Labels, l.Name)
		}
		out = append(out, is)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

type rawRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

type rawCompare struct {
	AheadBy int `json:"ahead_by"`
}

// release は最新リリースと、そのタグから branch がどれだけ進んでいるかを返す。
// リリースが1つも無ければ nil を返す（それは異常ではない）。
func (c *Client) release(ctx context.Context, repo, branch string) (*model.Release, error) {
	var raw rawRelease
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/releases/latest", repo), &raw); err != nil {
		if httpx.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	rel := &model.Release{Tag: raw.TagName, URL: raw.HTMLURL, PublishedAt: raw.PublishedAt}
	if branch == "" {
		branch = "main"
	}
	var cmp rawCompare
	path := fmt.Sprintf("/repos/%s/compare/%s...%s", repo, url.PathEscape(raw.TagName), url.PathEscape(branch))
	if err := c.get(ctx, path, &cmp); err != nil {
		// 比較できなくてもリリース自体の情報は返す。
		return rel, nil
	}
	rel.CommitsSince = cmp.AheadBy
	return rel, nil
}

func (c *Client) limit() int {
	if c.MaxPulls <= 0 {
		return 20
	}
	return c.MaxPulls
}
