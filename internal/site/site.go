// Package site は公開しているサイトが返事をするかを見る。
package site

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/finalize/mimawari/internal/model"
)

// Checker はサイトの死活を測る。
type Checker struct {
	HTTP *http.Client
}

// New は Checker を返す。
func New(hc *http.Client) *Checker { return &Checker{HTTP: hc} }

// Check は url に GET して、状態コードと応答までの時間を返す。
//
// HEAD ではなく GET にしてある。静的アセットを配る Worker は HEAD に
// 405 を返すことがあり、落ちているように見えてしまうため。
// 本文は読み捨てるが、最初の1バイトが返るまでを測っている。
func (c *Checker) Check(ctx context.Context, url string) *model.Site {
	if url == "" {
		return nil
	}
	out := &model.Site{URL: url}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	req.Header.Set("User-Agent", "mimawari")

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	start := time.Now()
	res, err := hc.Do(req)
	if err != nil {
		out.LatencyMS = time.Since(start).Milliseconds()
		out.Error = err.Error()
		return out
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	out.LatencyMS = time.Since(start).Milliseconds()
	out.Status = res.StatusCode
	out.OK = res.StatusCode >= 200 && res.StatusCode < 400
	return out
}
