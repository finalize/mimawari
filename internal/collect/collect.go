// Package collect は設定に並んだプロジェクトを一斉に見回る。
package collect

import (
	"context"
	"sync"
	"time"

	"github.com/finalize/mimawari/internal/config"
	"github.com/finalize/mimawari/internal/gh"
	"github.com/finalize/mimawari/internal/model"
	"github.com/finalize/mimawari/internal/npmreg"
	"github.com/finalize/mimawari/internal/report"
	"github.com/finalize/mimawari/internal/site"
)

// Collector は1回の見回りに必要なものを束ねる。
type Collector struct {
	Cfg    config.Config
	GitHub *gh.Client
	NPM    *npmreg.Client
	Site   *site.Checker
	// Now は時刻の取得。テストで固定するために差し替えられるようにしている。
	Now func() time.Time
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Run は全プロジェクトを並列に見回って Snapshot を返す。
//
// プロジェクトごとに goroutine を立て、その中で GitHub・npm・サイトの3系統を
// さらに並列に取る。どこが失敗しても Snapshot は返す。失敗は Errors に入る。
func (c *Collector) Run(ctx context.Context) model.Snapshot {
	start := c.now()
	projects := make([]model.Project, len(c.Cfg.Projects))

	var (
		mu   sync.Mutex
		errs []model.SourceError
		wg   sync.WaitGroup
	)
	record := func(source string, list []error) {
		if len(list) == 0 {
			return
		}
		mu.Lock()
		for _, err := range list {
			errs = append(errs, model.SourceError{Source: source, Message: err.Error()})
		}
		mu.Unlock()
	}

	for i, p := range c.Cfg.Projects {
		wg.Add(1)
		go func(i int, p config.Project) {
			defer wg.Done()
			projects[i] = c.one(ctx, p, record)
		}(i, p)
	}
	wg.Wait()

	snap := model.Snapshot{
		GeneratedAt: start,
		TookMS:      c.now().Sub(start).Milliseconds(),
		Projects:    projects,
		Errors:      errs,
	}
	snap.Attention = report.Attention(snap.Projects, c.Cfg.Thresholds, c.now())
	return snap
}

func (c *Collector) one(ctx context.Context, p config.Project, record func(string, []error)) model.Project {
	// 取れなかったときに JSON が null にならないよう、空スライスで始める。
	out := model.Project{
		Name:   p.Name,
		Repo:   p.Repo,
		Pulls:  []model.PullRequest{},
		Issues: []model.Issue{},
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		if c.GitHub == nil {
			return
		}
		repo, errs := c.GitHub.Fetch(ctx, p.Repo, p.Branch)
		record("github:"+p.Repo, errs)
		out.Release = repo.Release
		if repo.Pulls != nil {
			out.Pulls = repo.Pulls
		}
		if repo.Issues != nil {
			out.Issues = repo.Issues
		}
	}()

	go func() {
		defer wg.Done()
		if c.NPM == nil || p.NPM == "" {
			return
		}
		n, errs := c.NPM.Fetch(ctx, p.NPM, c.Cfg.ManifestPath(p))
		record("npm:"+p.NPM, errs)
		out.NPM = n
	}()

	go func() {
		defer wg.Done()
		if c.Site == nil || p.Site == "" {
			return
		}
		out.Site = c.Site.Check(ctx, p.Site)
	}()

	wg.Wait()
	return out
}
