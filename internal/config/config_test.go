package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("既定の設定を読めません: %v", err)
	}
	if len(c.Projects) == 0 {
		t.Fatal("projects が空です")
	}
	for _, p := range c.Projects {
		if p.Name == "" || p.Repo == "" || p.Branch == "" {
			t.Errorf("既定値が埋まっていません: %+v", p)
		}
	}
	if strings.HasPrefix(c.Workspace, "~") {
		t.Errorf("workspace の ~ が展開されていません: %q", c.Workspace)
	}
}

func TestParseFills(t *testing.T) {
	c, err := parse([]byte(`{"projects":[{"repo":"finalize/termpic"}]}`), "")
	if err != nil {
		t.Fatalf("読めません: %v", err)
	}
	p := c.Projects[0]
	if p.Name != "termpic" {
		t.Errorf("name = %q, 期待は repo から補った termpic", p.Name)
	}
	if p.Branch != "main" {
		t.Errorf("branch = %q, 期待は main", p.Branch)
	}
	if c.Thresholds.StalePRHours != 24 || c.Thresholds.StaleIssueDays != 14 || c.Thresholds.SlowSiteMS != 1500 {
		t.Errorf("しきい値の既定値が入っていません: %+v", c.Thresholds)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"projects が空", `{"projects":[]}`, "projects が空"},
		{"repo が無い", `{"projects":[{"name":"a"}]}`, "repo は必須"},
		{"repo の形式", `{"projects":[{"repo":"termpic"}]}`, "owner/name"},
		{"name の重複", `{"projects":[{"repo":"a/x"},{"repo":"b/x"}]}`, "重複"},
		{"manifest だけ", `{"projects":[{"repo":"a/x","manifest":"p.json"}]}`, "npm も指定"},
		{"知らないキー", `{"projects":[{"repo":"a/x"}],"nope":1}`, "形式が正しくありません"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse([]byte(tt.in), "")
			if err == nil {
				t.Fatal("エラーになるはずです")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("エラー = %v, %q を含むはず", err, tt.want)
			}
		})
	}
}

func TestManifestPath(t *testing.T) {
	c := Config{Workspace: "/w"}
	if got := c.ManifestPath(Project{}); got != "" {
		t.Errorf("manifest 未指定は空を返すはず: %q", got)
	}
	if got := c.ManifestPath(Project{Manifest: "a/package.json"}); got != "/w/a/package.json" {
		t.Errorf("相対パス = %q", got)
	}
	if got := c.ManifestPath(Project{Manifest: "/abs/package.json"}); got != "/abs/package.json" {
		t.Errorf("絶対パスはそのまま返すはず: %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	home := "/Users/x"
	tests := [][2]string{
		{"~", home},
		{"~/workspace", filepath.Join(home, "workspace")},
		{"/abs", "/abs"},
		{"", ""},
		{"~notme", "~notme"},
	}
	for _, tt := range tests {
		if got := expandHome(tt[0], home); got != tt[1] {
			t.Errorf("expandHome(%q) = %q, 期待は %q", tt[0], got, tt[1])
		}
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	if err := os.WriteFile(path, []byte(`{"workspace":"/w","projects":[{"repo":"a/b","site":"https://x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("読めません: %v", err)
	}
	if c.Projects[0].Site != "https://x" {
		t.Errorf("site = %q", c.Projects[0].Site)
	}
	if _, err := Load(filepath.Join(dir, "無い.json")); err == nil {
		t.Error("存在しないファイルはエラーになるはずです")
	}
}
