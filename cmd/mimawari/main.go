// mimawari は自分の個人開発を横断して見回り、手を動かすべきところを1つにまとめて出す。
//
//	mimawari              見回って出す
//	mimawari -json        JSON で出す
//	mimawari -exit-code   action があれば終了コード 1
//
// 作った動機は、Dependabot の PR が blocked のまま4日間気づかれずに
// 止まっていたこと。1リポジトリずつ見に行かないと分からない状態が、
// リポジトリが増えるほど当たり前に取りこぼされる。
package main

import (
	"context"
	"encoding/json"
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

	"github.com/finalize/mimawari/internal/collect"
	"github.com/finalize/mimawari/internal/config"
	"github.com/finalize/mimawari/internal/gh"
	"github.com/finalize/mimawari/internal/npmreg"
	"github.com/finalize/mimawari/internal/report"
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
		cfgPath  = flag.String("config", "", "設定ファイル（省略すると埋め込みの既定値）")
		timeout  = flag.Duration("timeout", 30*time.Second, "1回の見回りの制限時間")
		asJSON   = flag.Bool("json", false, "JSON で出す")
		colorOpt = flag.String("color", "auto", "色を付けるか（auto / always / never）")
		exitCode = flag.Bool("exit-code", false, "action が1件でもあれば終了コード 1")
		quiet    = flag.Bool("quiet", false, "手を動かすところだけ出す")
	)
	flag.Usage = usage
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	// 同じホストへ一斉に投げるので、既定の2本だと並列にならない。
	hc := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 16,
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}

	c := &collect.Collector{
		Cfg:    cfg,
		GitHub: gh.New(githubToken(), hc),
		NPM:    npmreg.New(hc),
		Site:   site.New(hc),
	}

	// Ctrl-C で ctx が終わり、走っている問い合わせが一斉に打ち切られる。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	snap := c.Run(ctx)

	switch {
	case *asJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(snap); err != nil {
			return err
		}
	case *quiet:
		fmt.Print(report.AttentionText(snap, useColor(*colorOpt)))
	default:
		fmt.Print(report.Text(snap, useColor(*colorOpt)))
	}

	if *exitCode && snap.HasAction() {
		os.Exit(1)
	}
	return nil
}

func usage() {
	fmt.Fprint(flag.CommandLine.Output(), strings.Join([]string{
		"mimawari — 個人開発を横断して見回り、手を動かすところをまとめて出す",
		"",
		"  mimawari [フラグ]",
		"",
		"フラグ:",
		"",
	}, "\n"))
	flag.PrintDefaults()
	fmt.Fprint(flag.CommandLine.Output(), strings.Join([]string{
		"",
		"GitHub のトークンは GITHUB_TOKEN → GH_TOKEN → `gh auth token` の順に探します。",
		"どれも無ければ未認証で動きます（公開リポジトリのみ・レート上限は低い）。",
		"",
	}, "\n"))
}

// githubToken は環境変数、無ければ gh CLI からトークンを読む。
// どちらも無ければ空を返す（公開リポジトリだけなら未認証でも動く）。
func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
