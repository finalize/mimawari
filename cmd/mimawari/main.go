// mimawari は自分の個人開発を横断して見回り、手を動かすべきところを1つにまとめる。
//
//	mimawari            # サーバとして起動する
//	mimawari -once      # 1回見回って端末に出して終わる
//
// 作った動機は、Dependabot の PR が blocked のまま4日間気づかれずに
// 止まっていたこと。1リポジトリずつ見に行かないと分からない状態が、
// リポジトリが増えるほど当たり前に取りこぼされる。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/finalize/mimawari/internal/cache"
	"github.com/finalize/mimawari/internal/collect"
	"github.com/finalize/mimawari/internal/config"
	"github.com/finalize/mimawari/internal/gh"
	"github.com/finalize/mimawari/internal/model"
	"github.com/finalize/mimawari/internal/npmreg"
	"github.com/finalize/mimawari/internal/report"
	"github.com/finalize/mimawari/internal/server"
	"github.com/finalize/mimawari/internal/site"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mimawari:", err)
		os.Exit(2)
	}
}

func run() error {
	var (
		addr     = flag.String("addr", "127.0.0.1:8787", "待ち受けるアドレス")
		cfgPath  = flag.String("config", "", "設定ファイル（省略すると埋め込みの既定値）")
		ttl      = flag.Duration("ttl", 90*time.Second, "結果を持ち回す時間")
		timeout  = flag.Duration("timeout", 30*time.Second, "1回の見回りの制限時間")
		once     = flag.Bool("once", false, "1回だけ見回って終わる")
		asJSON   = flag.Bool("json", false, "-once のときに JSON で出す")
		colorOpt = flag.String("color", "auto", "色を付けるか（auto / always / never）")
		exitCode = flag.Bool("exit-code", false, "-once のとき、action があれば終了コード 1")
	)
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	token, tokenSrc := githubToken()
	hc := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			// 同じホストへ一斉に投げるので、既定の2本だと並列にならない。
			MaxIdleConnsPerHost: 16,
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}

	c := &collect.Collector{
		Cfg:    cfg,
		GitHub: gh.New(token, hc),
		NPM:    npmreg.New(hc),
		Site:   site.New(hc),
	}
	fetch := func(ctx context.Context) model.Snapshot {
		ctx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		return c.Run(ctx)
	}

	if *once {
		return runOnce(fetch, *asJSON, useColor(*colorOpt), *exitCode)
	}

	if token == "" {
		fmt.Fprintln(os.Stderr, "mimawari: GitHub のトークンが無いので、公開リポジトリのみ・低いレート上限で動きます")
	} else {
		fmt.Fprintf(os.Stderr, "mimawari: GitHub のトークンを %s から読みました\n", tokenSrc)
	}
	return serve(*addr, cache.New(*ttl, fetch), *ttl)
}

func runOnce(fetch cache.Fetch, asJSON, color, exitCode bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	snap := fetch(ctx)
	if asJSON {
		writeJSONTo(os.Stdout, snap)
	} else {
		fmt.Print(report.Text(snap, color))
	}
	if exitCode && snap.HasAction() {
		os.Exit(1)
	}
	return nil
}

func serve(addr string, c *cache.Cache, ttl time.Duration) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(c),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "mimawari: http://%s で待ち受けます（キャッシュ %s）\n", addr, ttl)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fmt.Fprintln(os.Stderr, "\nmimawari: 終了します")
	return srv.Shutdown(shutCtx)
}

// githubToken は環境変数、無ければ gh CLI からトークンを読む。
// どちらも無ければ空を返す（公開リポジトリだけなら未認証でも動く）。
func githubToken() (token, source string) {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v, "環境変数 " + k
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return "", ""
	}
	return strings.TrimSpace(string(out)), "gh auth token"
}

// useColor は色を付けるかを決める。auto のときは端末に出しているかで判断する。
func useColor(opt string) bool {
	switch opt {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func writeJSONTo(f *os.File, snap model.Snapshot) {
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snap)
}
