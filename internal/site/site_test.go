package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		wantOK bool
	}{
		{"200 は生きている", 200, true},
		{"301 も生きている", 301, true},
		{"404 は落ちている扱い", 404, false},
		{"500 は落ちている", 500, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("メソッド = %s, GET のはず", r.Method)
				}
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			got := New(srv.Client()).Check(context.Background(), srv.URL)
			if got.OK != tt.wantOK {
				t.Errorf("OK = %v, 期待は %v", got.OK, tt.wantOK)
			}
			if got.Status != tt.code {
				t.Errorf("Status = %d, 期待は %d", got.Status, tt.code)
			}
			if got.LatencyMS < 0 {
				t.Errorf("LatencyMS = %d", got.LatencyMS)
			}
		})
	}
}

func TestCheckUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 閉じてから叩く

	got := New(srv.Client()).Check(context.Background(), url)
	if got.OK {
		t.Error("届かないのに OK になっています")
	}
	if got.Error == "" {
		t.Error("Error が空です")
	}
}

func TestCheckEmptyURL(t *testing.T) {
	if got := New(nil).Check(context.Background(), ""); got != nil {
		t.Errorf("URL 未設定は nil を返すはず: %+v", got)
	}
}
