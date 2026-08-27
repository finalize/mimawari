// Package config は見回る対象の定義を読む。
//
// 何も指定しなければ default.json を埋め込んだものを使う。
// プロジェクトが増えたらこのファイルに足すか、-config で別のファイルを渡す。
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed default.json
var defaultJSON []byte

// Project は見回る対象1件。repo 以外は省略できる。
type Project struct {
	Name   string `json:"name"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Site   string `json:"site,omitempty"`
	NPM    string `json:"npm,omitempty"`
	// Manifest は Workspace からの相対パスで指す package.json。
	// npm に公開済みのバージョンと突き合わせて、公開し忘れを見つけるために使う。
	Manifest string `json:"manifest,omitempty"`
}

// Owner はリポジトリの所有者を返す。
func (p Project) Owner() string {
	owner, _, _ := strings.Cut(p.Repo, "/")
	return owner
}

// Thresholds は「気にし始める」境目。
type Thresholds struct {
	StalePRHours   int `json:"stale_pr_hours"`
	StaleIssueDays int `json:"stale_issue_days"`
	SlowSiteMS     int `json:"slow_site_ms"`
}

// Config は設定ファイル全体。
type Config struct {
	Workspace  string     `json:"workspace"`
	Thresholds Thresholds `json:"thresholds"`
	Projects   []Project  `json:"projects"`
}

// ManifestPath は Manifest の絶対パスを返す。指定が無ければ空文字。
func (c Config) ManifestPath(p Project) string {
	if p.Manifest == "" {
		return ""
	}
	if filepath.IsAbs(p.Manifest) {
		return p.Manifest
	}
	return filepath.Join(c.Workspace, p.Manifest)
}

// Load は path の設定を読む。path が空なら埋め込みの既定値を使う。
func Load(path string) (Config, error) {
	raw := defaultJSON
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("設定ファイルを読めません: %w", err)
		}
		raw = b
	}
	return parse(raw, path)
}

func parse(raw []byte, path string) (Config, error) {
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		where := path
		if where == "" {
			where = "default.json"
		}
		return Config{}, fmt.Errorf("%s の形式が正しくありません: %w", where, err)
	}
	if err := c.normalize(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c *Config) normalize() error {
	if len(c.Projects) == 0 {
		return fmt.Errorf("projects が空です")
	}
	home, _ := os.UserHomeDir()
	c.Workspace = expandHome(c.Workspace, home)

	if c.Thresholds.StalePRHours <= 0 {
		c.Thresholds.StalePRHours = 24
	}
	if c.Thresholds.StaleIssueDays <= 0 {
		c.Thresholds.StaleIssueDays = 14
	}
	if c.Thresholds.SlowSiteMS <= 0 {
		c.Thresholds.SlowSiteMS = 1500
	}

	seen := make(map[string]bool, len(c.Projects))
	for i := range c.Projects {
		p := &c.Projects[i]
		if p.Repo == "" {
			return fmt.Errorf("projects[%d]: repo は必須です", i)
		}
		owner, name, ok := strings.Cut(p.Repo, "/")
		if !ok || owner == "" || name == "" {
			return fmt.Errorf("projects[%d]: repo は owner/name の形式で書きます（%q）", i, p.Repo)
		}
		if p.Name == "" {
			p.Name = name
		}
		if seen[p.Name] {
			return fmt.Errorf("projects[%d]: name %q が重複しています", i, p.Name)
		}
		seen[p.Name] = true
		if p.Branch == "" {
			p.Branch = "main"
		}
		if p.Manifest != "" && p.NPM == "" {
			return fmt.Errorf("projects[%d]: manifest を指定するなら npm も指定します", i)
		}
	}
	return nil
}

// expandHome は先頭の ~ をホームディレクトリに置き換える。
func expandHome(path, home string) string {
	if home == "" || path == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
