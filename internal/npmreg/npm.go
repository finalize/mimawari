// Package npmreg は npm レジストリから公開済みバージョンと先週のダウンロード数を取る。
//
// 手元の package.json を上げたまま publish し忘れる、という取りこぼしを
// 見つけるのが主な目的なので、ローカルの manifest も読む。
package npmreg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/finalize/mimawari/internal/httpx"
	"github.com/finalize/mimawari/internal/model"
)

const (
	// DefaultRegistry は公開レジストリ。
	DefaultRegistry = "https://registry.npmjs.org"
	// DefaultDownloadsAPI はダウンロード数の API（レジストリとは別ホスト）。
	DefaultDownloadsAPI = "https://api.npmjs.org"
)

// Client は npm の2つの API を叩く。
type Client struct {
	Registry     string
	DownloadsAPI string
	HTTP         *http.Client
}

// New は既定の設定の Client を返す。
func New(hc *http.Client) *Client {
	return &Client{Registry: DefaultRegistry, DownloadsAPI: DefaultDownloadsAPI, HTTP: hc}
}

// Fetch は pkg の状態を返す。manifestPath が空でなければローカルのバージョンも読む。
//
// ダウンロード数だけ取れなくても、公開バージョンが取れていれば結果は返す。
func (c *Client) Fetch(ctx context.Context, pkg, manifestPath string) (*model.NPM, []error) {
	if pkg == "" {
		return nil, nil
	}
	out := &model.NPM{
		Package: pkg,
		URL:     "https://www.npmjs.com/package/" + pkg,
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	fail := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		v, err := c.latest(ctx, pkg)
		if err != nil {
			fail(fmt.Errorf("npm %s の公開バージョン: %w", pkg, err))
			return
		}
		mu.Lock()
		out.Published = v
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		n, err := c.downloads(ctx, pkg)
		if err != nil {
			fail(fmt.Errorf("npm %s のダウンロード数: %w", pkg, err))
			return
		}
		mu.Lock()
		out.DownloadsLastWeek = n
		mu.Unlock()
	}()
	wg.Wait()

	if manifestPath != "" {
		v, err := LocalVersion(manifestPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", manifestPath, err))
		} else {
			out.Local = v
		}
	}
	// バージョン文字列の大小ではなく一致だけを見る。
	// 「手元と公開が違う」時点で人が確かめるべきことなので、
	// semver の比較を持ち込んで前後を判定するほどの意味はない。
	out.Drift = out.Local != "" && out.Published != "" && out.Local != out.Published
	return out, errs
}

func (c *Client) latest(ctx context.Context, pkg string) (string, error) {
	base := c.Registry
	if base == "" {
		base = DefaultRegistry
	}
	var raw struct {
		Version string `json:"version"`
	}
	u := fmt.Sprintf("%s/%s/latest", base, escapePkg(pkg))
	if err := httpx.GetJSON(ctx, c.HTTP, u, nil, &raw); err != nil {
		return "", err
	}
	return raw.Version, nil
}

func (c *Client) downloads(ctx context.Context, pkg string) (int, error) {
	base := c.DownloadsAPI
	if base == "" {
		base = DefaultDownloadsAPI
	}
	var raw struct {
		Downloads int `json:"downloads"`
	}
	u := fmt.Sprintf("%s/downloads/point/last-week/%s", base, escapePkg(pkg))
	if err := httpx.GetJSON(ctx, c.HTTP, u, nil, &raw); err != nil {
		return 0, err
	}
	return raw.Downloads, nil
}

// escapePkg はスコープ付きパッケージ名（@scope/name）の / を残したままエスケープする。
func escapePkg(pkg string) string {
	return url.PathEscape(pkg)
}

// LocalVersion は package.json の version を読む。
func LocalVersion(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var raw struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", fmt.Errorf("package.json を読めません: %w", err)
	}
	return raw.Version, nil
}
