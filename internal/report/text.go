package report

import (
	"fmt"
	"strings"

	"github.com/finalize/mimawari/internal/model"
)

// 端末で読むときの色。個人サイトがターミナル風なので、出力もそちらに寄せている。
const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiGreen  = "\x1b[32m"
)

type painter struct{ on bool }

func (p painter) paint(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (p painter) sev(s model.Severity) string {
	label := map[model.Severity]string{
		model.SevAction: "action",
		model.SevWarn:   "warn  ",
		model.SevInfo:   "info  ",
	}[s]
	if label == "" {
		label = "?     "
	}
	switch s {
	case model.SevAction:
		return p.paint(ansiRed, label)
	case model.SevWarn:
		return p.paint(ansiYellow, label)
	default:
		return p.paint(ansiDim, label)
	}
}

// Text は Snapshot を端末で読める形にする。color が true なら色を付ける。
func Text(s model.Snapshot, color bool) string {
	p := painter{on: color}
	var b strings.Builder

	fmt.Fprintf(&b, "%s  %s  (%s, %d プロジェクト)\n",
		p.paint(ansiBold, "mimawari"),
		s.GeneratedAt.Local().Format("2006-01-02 15:04:05"),
		formatMS(s.TookMS),
		len(s.Projects))

	b.WriteString("\n")
	if len(s.Attention) == 0 {
		b.WriteString(p.paint(ansiGreen, "手を動かすところはありません") + "\n")
	} else {
		fmt.Fprintf(&b, "%s (%d)\n", p.paint(ansiBold, "手を動かすところ"), len(s.Attention))
		width := 0
		for _, it := range s.Attention {
			if len(it.Project) > width {
				width = len(it.Project)
			}
		}
		for _, it := range s.Attention {
			fmt.Fprintf(&b, "  %s  %-*s  %s\n", p.sev(it.Severity), width, it.Project, it.Summary)
			indent := strings.Repeat(" ", 2+6+2+width+2)
			if it.Detail != "" {
				fmt.Fprintf(&b, "%s%s\n", indent, p.paint(ansiDim, it.Detail))
			}
			if it.URL != "" {
				fmt.Fprintf(&b, "%s%s\n", indent, p.paint(ansiDim, it.URL))
			}
		}
	}

	b.WriteString("\n" + p.paint(ansiBold, "プロジェクト") + "\n")
	nameW := 0
	for _, pr := range s.Projects {
		if len(pr.Name) > nameW {
			nameW = len(pr.Name)
		}
	}
	for _, pr := range s.Projects {
		fmt.Fprintf(&b, "  %-*s  %s  %s  %s  %s\n",
			nameW, pr.Name,
			siteCell(p, pr.Site),
			fmt.Sprintf("pr %-2d", len(pr.Pulls)),
			fmt.Sprintf("issue %-2d", len(pr.Issues)),
			npmCell(pr.NPM))
	}

	if len(s.Errors) > 0 {
		fmt.Fprintf(&b, "\n%s (%d)\n", p.paint(ansiBold, "取れなかったもの"), len(s.Errors))
		for _, e := range s.Errors {
			fmt.Fprintf(&b, "  %s  %s\n", e.Source, p.paint(ansiDim, e.Message))
		}
	}
	return b.String()
}

// siteCell の幅は "site 200  138ms" と同じ 15 に揃える。
// 色を付けてから %-15s に渡すとエスケープ列まで幅に数えられてずれるので、
// 先に詰めてから色を付ける。
const siteCellWidth = 15

func siteCell(p painter, s *model.Site) string {
	if s == nil {
		return pad("site -", siteCellWidth)
	}
	if !s.OK {
		code := fmt.Sprintf("%d", s.Status)
		if s.Status == 0 {
			code = "err"
		}
		return p.paint(ansiRed, pad("site "+code, siteCellWidth))
	}
	return pad(fmt.Sprintf("site %3d %4dms", s.Status, s.LatencyMS), siteCellWidth)
}

func pad(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func npmCell(n *model.NPM) string {
	if n == nil {
		return "npm -"
	}
	published := n.Published
	if published == "" {
		published = "?"
	}
	out := "npm " + published
	if n.Drift {
		out += " (手元 " + n.Local + ")"
	}
	if n.DownloadsLastWeek > 0 {
		out += fmt.Sprintf(" 週%d", n.DownloadsLastWeek)
	}
	return out
}

func formatMS(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
