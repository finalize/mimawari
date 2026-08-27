package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/finalize/mimawari/internal/model"
)

func TestGetUsesTTL(t *testing.T) {
	clock := time.Date(2026, 8, 27, 22, 0, 0, 0, time.UTC)
	calls := 0
	c := New(90*time.Second, func(context.Context) model.Snapshot {
		calls++
		return model.Snapshot{TookMS: int64(calls)}
	})
	c.now = func() time.Time { return clock }

	if snap, cached := c.Get(context.Background(), false); cached || snap.TookMS != 1 {
		t.Fatalf("初回は取りに行くはず: cached=%v %+v", cached, snap)
	}
	if snap, cached := c.Get(context.Background(), false); !cached || snap.TookMS != 1 {
		t.Fatalf("TTL 内は持っているものを返すはず: cached=%v %+v", cached, snap)
	}
	if calls != 1 {
		t.Fatalf("取得回数 = %d, 期待は 1", calls)
	}

	clock = clock.Add(91 * time.Second)
	if snap, cached := c.Get(context.Background(), false); cached || snap.TookMS != 2 {
		t.Fatalf("TTL を過ぎたら取り直すはず: cached=%v %+v", cached, snap)
	}
}

func TestGetFreshBypasses(t *testing.T) {
	calls := 0
	c := New(time.Hour, func(context.Context) model.Snapshot {
		calls++
		return model.Snapshot{}
	})
	c.Get(context.Background(), false)
	if _, cached := c.Get(context.Background(), true); cached {
		t.Error("fresh=true は取り直すはず")
	}
	if calls != 2 {
		t.Errorf("取得回数 = %d, 期待は 2", calls)
	}
}

// 同時に来た要求で外の API を何度も叩かない。
func TestGetSerializesConcurrentCalls(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	c := New(time.Hour, func(context.Context) model.Snapshot {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return model.Snapshot{}
	})

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get(context.Background(), false)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("取得回数 = %d, 期待は 1", calls)
	}
}

func TestAge(t *testing.T) {
	clock := time.Date(2026, 8, 27, 22, 0, 0, 0, time.UTC)
	c := New(time.Hour, func(context.Context) model.Snapshot { return model.Snapshot{} })
	c.now = func() time.Time { return clock }

	if _, ok := c.Age(); ok {
		t.Error("何も取っていないのに Age が返っています")
	}
	c.Get(context.Background(), false)
	clock = clock.Add(30 * time.Second)
	age, ok := c.Age()
	if !ok || age != 30*time.Second {
		t.Errorf("Age = %v, %v", age, ok)
	}
}
