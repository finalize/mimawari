// Package httpx は JSON を返す API を叩くための小さな共通処理。
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrNotFound は 404 を表す。
//
// 「無いのが正常」な問い合わせ（リリースを一度も作っていないリポジトリなど）が
// あるので、失敗一般と区別できるようにしている。
var ErrNotFound = errors.New("見つかりません")

// StatusError は 2xx 以外の応答。
type StatusError struct {
	Code int
	URL  string
	Body string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s が %d を返しました", e.URL, e.Code)
	}
	return fmt.Sprintf("%s が %d を返しました: %s", e.URL, e.Code, e.Body)
}

// maxBody は読み込むレスポンスの上限。壊れた相手に付き合わされないための歯止め。
const maxBody = 8 << 20

// GetJSON は url を GET して JSON を v にデコードする。
// hdr は追加のリクエストヘッダ（nil 可）。
func GetJSON(ctx context.Context, c *http.Client, url string, hdr http.Header, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, vs := range hdr {
		for _, s := range vs {
			req.Header.Add(k, s)
		}
	}
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body := io.LimitReader(res.Body, maxBody)
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", url, ErrNotFound)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(body, 512))
		return &StatusError{Code: res.StatusCode, URL: url, Body: string(snippet)}
	}
	if v == nil {
		_, _ = io.Copy(io.Discard, body)
		return nil
	}
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("%s の応答を読めません: %w", url, err)
	}
	return nil
}

// IsNotFound は err が 404 由来かを返す。
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
