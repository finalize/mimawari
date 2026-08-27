package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" {
			t.Errorf("ヘッダが渡っていません: %q", r.Header.Get("X-Test"))
		}
		fmt.Fprint(w, `{"n":42}`)
	}))
	defer srv.Close()

	var out struct {
		N int `json:"n"`
	}
	hdr := http.Header{}
	hdr.Set("X-Test", "yes")
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, hdr, &out); err != nil {
		t.Fatal(err)
	}
	if out.N != 42 {
		t.Errorf("n = %d", out.N)
	}
}

// 404 は「無いのが正常」な問い合わせがあるので、失敗一般と区別できること。
func TestGetJSONNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	err := GetJSON(context.Background(), srv.Client(), srv.URL, nil, nil)
	if !errors.Is(err, ErrNotFound) || !IsNotFound(err) {
		t.Errorf("err = %v, ErrNotFound を包むはず", err)
	}
}

func TestGetJSONStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"message":"rate limit"}`)
	}))
	defer srv.Close()

	err := GetJSON(context.Background(), srv.Client(), srv.URL, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, StatusError のはず", err)
	}
	if se.Code != 429 || !strings.Contains(se.Body, "rate limit") {
		t.Errorf("StatusError = %+v", se)
	}
	if !strings.Contains(se.Error(), "429") {
		t.Errorf("メッセージ = %q", se.Error())
	}
}

func TestGetJSONBrokenBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{壊れている`)
	}))
	defer srv.Close()

	var out struct{}
	err := GetJSON(context.Background(), srv.Client(), srv.URL, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "応答を読めません") {
		t.Errorf("err = %v", err)
	}
}

func TestGetJSONContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := GetJSON(ctx, srv.Client(), srv.URL, nil, nil); err == nil {
		t.Error("打ち切られたら失敗するはず")
	}
}
