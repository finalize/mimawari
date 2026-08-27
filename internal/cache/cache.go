// Package cache は見回りの結果を一定時間だけ持ち回す。
//
// 1回の見回りで GitHub に数十回問い合わせるので、画面を開き直すたびに
// 走らせるとレート上限に当たる。TTL を過ぎるまでは同じ結果を返す。
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/finalize/mimawari/internal/model"
)

// Fetch は結果を作り直す関数。
type Fetch func(context.Context) model.Snapshot

// Cache は最後の結果と、それを取った時刻を持つ。
type Cache struct {
	ttl   time.Duration
	fetch Fetch
	now   func() time.Time

	// mu は取り直しのあいだも握ったままにする。
	// 同時に来た要求を並ばせることで、外の API を同時に何度も叩かない。
	mu    sync.Mutex
	val   model.Snapshot
	at    time.Time
	valid bool
}

// New は TTL と取得関数を指定して Cache を作る。
func New(ttl time.Duration, fetch Fetch) *Cache {
	return &Cache{ttl: ttl, fetch: fetch, now: time.Now}
}

// Get は結果を返す。fresh が true なら TTL を無視して取り直す。
// 2つ目の戻り値は、持っていたものをそのまま返したかどうか。
func (c *Cache) Get(ctx context.Context, fresh bool) (model.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !fresh && c.valid && c.now().Sub(c.at) < c.ttl {
		return c.val, true
	}
	snap := c.fetch(ctx)
	c.val, c.at, c.valid = snap, c.now(), true
	return snap, false
}

// Age は持っている結果の古さを返す。まだ何も無ければ false。
func (c *Cache) Age() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid {
		return 0, false
	}
	return c.now().Sub(c.at), true
}
