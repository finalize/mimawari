package npmreg

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeNPM(t *testing.T, version string, downloads int, downCode int) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{pkg}/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":%q}`, version)
	})
	mux.HandleFunc("GET /downloads/point/last-week/{pkg}", func(w http.ResponseWriter, r *http.Request) {
		if downCode != 200 {
			w.WriteHeader(downCode)
			return
		}
		fmt.Fprintf(w, `{"downloads":%d,"package":"x"}`, downloads)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{Registry: srv.URL, DownloadsAPI: srv.URL, HTTP: srv.Client()}
}

func writeManifest(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"name":"termpic","version":%q}`, version)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFetch(t *testing.T) {
	tests := []struct {
		name      string
		published string
		local     string
		wantDrift bool
	}{
		{"一致していれば問題なし", "0.2.2", "0.2.2", false},
		{"手元が先に進んでいる（公開し忘れ）", "0.2.1", "0.2.2", true},
		{"公開の方が新しい（手元が古い）", "0.3.0", "0.2.2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakeNPM(t, tt.published, 775, 200)
			got, errs := c.Fetch(context.Background(), "termpic", writeManifest(t, tt.local))
			if len(errs) != 0 {
				t.Fatalf("エラー: %v", errs)
			}
			if got.Published != tt.published || got.Local != tt.local {
				t.Errorf("published = %q, local = %q", got.Published, got.Local)
			}
			if got.Drift != tt.wantDrift {
				t.Errorf("Drift = %v, 期待は %v", got.Drift, tt.wantDrift)
			}
			if got.DownloadsLastWeek != 775 {
				t.Errorf("ダウンロード数 = %d", got.DownloadsLastWeek)
			}
			if got.URL != "https://www.npmjs.com/package/termpic" {
				t.Errorf("URL = %q", got.URL)
			}
		})
	}
}

func TestFetchWithoutManifest(t *testing.T) {
	c := fakeNPM(t, "0.3.0", 542, 200)
	got, errs := c.Fetch(context.Background(), "contrast-kit", "")
	if len(errs) != 0 {
		t.Fatalf("エラー: %v", errs)
	}
	if got.Local != "" || got.Drift {
		t.Errorf("手元を見ないなら比較しないはず: %+v", got)
	}
}

// ダウンロード数が取れなくても、公開バージョンが取れていれば結果は返す。
func TestFetchDownloadsFailure(t *testing.T) {
	c := fakeNPM(t, "0.2.2", 0, 500)
	got, errs := c.Fetch(context.Background(), "termpic", "")
	if len(errs) != 1 {
		t.Fatalf("エラー数 = %d, 期待は 1: %v", len(errs), errs)
	}
	if got == nil || got.Published != "0.2.2" {
		t.Fatalf("公開バージョンは返るはず: %+v", got)
	}
}

func TestFetchMissingManifest(t *testing.T) {
	c := fakeNPM(t, "0.2.2", 1, 200)
	got, errs := c.Fetch(context.Background(), "termpic", "/存在しない/package.json")
	if len(errs) != 1 {
		t.Fatalf("エラー数 = %d: %v", len(errs), errs)
	}
	if got.Drift {
		t.Error("読めなかったものを差分にしてはいけません")
	}
}

func TestFetchEmptyPackage(t *testing.T) {
	c := fakeNPM(t, "1.0.0", 1, 200)
	got, errs := c.Fetch(context.Background(), "", "")
	if got != nil || errs != nil {
		t.Errorf("パッケージ未設定は nil のはず: %+v %v", got, errs)
	}
}

func TestLocalVersionBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(path, []byte(`{壊れている`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LocalVersion(path)
	if err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Errorf("err = %v", err)
	}
}
