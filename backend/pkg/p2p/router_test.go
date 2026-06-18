// ─── p2p Router 单元测试 ─────────────────────────────────────────────────────
//
// Router 是纯内存数据结构，无需真实网络连接，用例覆盖：
//   路由添加/查询、广播处理、过期清理、PEX 列表管理

package p2p

import (
	"fmt"
	"testing"
	"time"
)

// ===== NewRouter / AddDirect / NextHop =====

func TestNewRouterHasSelf(t *testing.T) {
	r := NewRouter("node-self", "127.0.0.1:9000")
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 route (self), got %d", len(snap))
	}
	if snap[0].Target != "node-self" {
		t.Errorf("self target: got %q", snap[0].Target)
	}
	if snap[0].Distance != 0 {
		t.Errorf("self distance: got %d, want 0", snap[0].Distance)
	}
}

func TestAddDirect(t *testing.T) {
	r := NewRouter("self", ":0")
	r.AddDirect("peer-1", "192.168.1.10:9001")

	hop, dist := r.NextHop("peer-1")
	if hop != "192.168.1.10:9001" {
		t.Errorf("NextHop: got %q, want %q", hop, "192.168.1.10:9001")
	}
	if dist != 1 {
		t.Errorf("Distance: got %d, want 1", dist)
	}
}

func TestAddDirectSelfIgnored(t *testing.T) {
	r := NewRouter("self", ":0")
	r.AddDirect("self", "1.2.3.4:5678")

	// 不应该覆盖到自己路由
	_, dist := r.NextHop("self")
	if dist != 0 {
		t.Errorf("self distance should remain 0, got %d", dist)
	}
}

func TestAddDirectEmptyIgnored(t *testing.T) {
	r := NewRouter("self", ":0")
	r.AddDirect("", "")
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Errorf("expected only self route, got %d", len(snap))
	}
}

func TestNextHopUnknown(t *testing.T) {
	r := NewRouter("self", ":0")
	hop, dist := r.NextHop("non-existent")
	if hop != "" {
		t.Errorf("NextHop for unknown: got %q, want empty", hop)
	}
	if dist != -1 {
		t.Errorf("Distance for unknown: got %d, want -1", dist)
	}
}

// ===== ApplyAdvertise =====

func TestApplyAdvertiseNewRoute(t *testing.T) {
	r := NewRouter("self", ":0")

	// 邻居 A 宣告：它到目标 C 距离=1
	adv := RouteAdvertise{
		From:     "node-a",
		FromAddr: "10.0.0.1:8000",
		Entries: []RouteEntry{
			{Target: "node-c", NextHop: "10.0.0.1:8000", Distance: 1},
		},
	}
	updated := r.ApplyAdvertise("node-a", "10.0.0.1:8000", adv)
	if updated != 1 {
		t.Fatalf("expected 1 updated route, got %d", updated)
	}

	// 经过 A 到 C：distance = 1(邻居到C) + 1(我到邻居) = 2
	hop, dist := r.NextHop("node-c")
	if hop != "10.0.0.1:8000" {
		t.Errorf("NextHop to C: got %q", hop)
	}
	if dist != 2 {
		t.Errorf("Distance to C: got %d, want 2", dist)
	}
}

func TestApplyAdvertiseShorterPath(t *testing.T) {
	r := NewRouter("self", ":0")

	// 先有一条较长的路：自我到 A(距离=1) → A到目标(距离=2)，总计 3
	r.AddDirect("node-a", "10.0.0.1:8000")
	r.ApplyAdvertise("node-a", "10.0.0.1:8000", RouteAdvertise{
		Entries: []RouteEntry{{Target: "target-z", Distance: 2}},
	})
	// 验证当前距离=3
	_, dist := r.NextHop("target-z")
	if dist != 3 {
		t.Fatalf("initial distance to Z: got %d, want 3", dist)
	}

	// B 宣告：它到目标距离=1 → 总距离 2
	updated := r.ApplyAdvertise("node-b", "10.0.0.2:8000", RouteAdvertise{
		Entries: []RouteEntry{{Target: "target-z", Distance: 1}},
	})
	if updated != 1 {
		t.Errorf("expected 1 update for shorter path, got %d", updated)
	}
	hop, dist := r.NextHop("target-z")
	if hop != "10.0.0.2:8000" {
		t.Errorf("NextHop for shorter path: got %q, want %q", hop, "10.0.0.2:8000")
	}
	if dist != 2 {
		t.Errorf("Distance for shorter path: got %d, want 2", dist)
	}
}

func TestApplyAdvertiseSelfIgnored(t *testing.T) {
	r := NewRouter("self", ":0")

	adv := RouteAdvertise{
		From: "neighbor",
		Entries: []RouteEntry{
			{Target: "self", Distance: 1}, // 宣告到"我"的路由 → 应忽略
		},
	}
	updated := r.ApplyAdvertise("neighbor", "10.0.0.1:8000", adv)
	if updated != 0 {
		t.Errorf("expected 0 updates (self ignored), got %d", updated)
	}
}

func TestApplyAdvertiseMaxDistanceBound(t *testing.T) {
	r := NewRouter("self", ":0")

	adv := RouteAdvertise{
		From: "far-node",
		Entries: []RouteEntry{
			{Target: "too-far", Distance: defaultMaxDistance}, // 8+1=9 > 8
		},
	}
	updated := r.ApplyAdvertise("far-node", "10.0.0.1:8000", adv)
	if updated != 0 {
		t.Errorf("expected 0 updates (beyond max distance), got %d", updated)
	}
}

// ===== BuildAdvertise =====

func TestBuildAdvertise(t *testing.T) {
	r := NewRouter("self", ":9999")
	r.AddDirect("peer-1", "10.0.0.1:8000")
	r.AddDirect("peer-2", "10.0.0.2:8000")

	adv := r.BuildAdvertise()
	if adv.From != "self" {
		t.Errorf("advertise From: got %q", adv.From)
	}
	if adv.FromAddr != ":9999" {
		t.Errorf("advertise FromAddr: got %q", adv.FromAddr)
	}
	// 自己 + 2 个邻居
	if len(adv.Entries) != 3 {
		t.Errorf("expected 3 entries (self+2peers), got %d", len(adv.Entries))
	}

	// 验证自己的条目 distance=0
	for _, e := range adv.Entries {
		if e.Target == "self" && e.Distance != 0 {
			t.Errorf("self distance in advertise: got %d, want 0", e.Distance)
		}
	}
}

// ===== CleanupExpired =====

func TestCleanupExpiredRemovesOldEntries(t *testing.T) {
	r := NewRouter("self", ":0")
	r.AddDirect("peer-stale", "10.0.0.1:8000")

	// 手动把 peer-stale 的时间戳设成过期
	r.mu.Lock()
	if e, ok := r.routes["peer-stale"]; ok {
		e.Updated = time.Now().Unix() - int64(routeExpire/time.Second) - 10
		r.routes["peer-stale"] = e
	}
	r.mu.Unlock()

	deleted := r.CleanupExpired()
	if deleted != 1 {
		t.Errorf("expected 1 expired entry deleted, got %d", deleted)
	}

	// 验证已删除
	_, dist := r.NextHop("peer-stale")
	if dist != -1 {
		t.Error("expired peer should have been removed")
	}

	// 自己（distance=0）不能被删除
	_, dist = r.NextHop("self")
	if dist != 0 {
		t.Errorf("self distance altered: got %d, want 0", dist)
	}
}

func TestCleanupExpiredFreshEntry(t *testing.T) {
	r := NewRouter("self", ":0")
	r.AddDirect("peer-fresh", "10.0.0.1:8000")

	deleted := r.CleanupExpired()
	if deleted != 0 {
		t.Errorf("expected 0 deleted for fresh entries, got %d", deleted)
	}
}

// ===== Snapshot =====

func TestSnapshotOrdering(t *testing.T) {
	r := NewRouter("self", ":0")
	r.AddDirect("peer-b", "10.0.0.1:8000")
	r.AddDirect("peer-a", "10.0.0.2:8000")

	snap := r.Snapshot()
	// 按 distance 升序，同 distance 按 target 字母序
	// self(dist=0), peer-a(dist=1), peer-b(dist=1)
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}
	if snap[0].Target != "self" {
		t.Errorf("snap[0]: got %q, want 'self'", snap[0].Target)
	}
	if snap[1].Target != "peer-a" || snap[2].Target != "peer-b" {
		t.Errorf("snap order: got %q, %q; want 'peer-a', 'peer-b'",
			snap[1].Target, snap[2].Target)
	}
}

// ===== PexSet =====

func TestNewPexSetEmpty(t *testing.T) {
	p := NewPexSet("self")
	if p.Size() != 0 {
		t.Errorf("new PexSet size: got %d, want 0", p.Size())
	}
}

func TestPexIngest(t *testing.T) {
	p := NewPexSet("self")

	pe := &PeerExchange{
		From: "node-a",
		Peers: []PeerExchangeItem{
			{ID: "node-b", Addr: "10.0.0.2:8000"},
			{ID: "node-c", Addr: "10.0.0.3:8000"},
		},
	}
	added := p.Ingest(pe)
	if added != 2 {
		t.Errorf("expected 2 added, got %d", added)
	}
	if p.Size() != 2 {
		t.Errorf("size after ingest: got %d, want 2", p.Size())
	}
}

func TestPexIngestDuplicate(t *testing.T) {
	p := NewPexSet("self")

	p.Ingest(&PeerExchange{From: "a", Peers: []PeerExchangeItem{
		{ID: "node-x", Addr: "10.0.0.1:8000"},
	}})
	added := p.Ingest(&PeerExchange{From: "b", Peers: []PeerExchangeItem{
		{ID: "node-x", Addr: "10.0.0.1:8000"}, // 重复
	}})
	if added != 0 {
		t.Errorf("expected 0 added for duplicate, got %d", added)
	}
}

func TestPexIngestSelfIgnored(t *testing.T) {
	p := NewPexSet("self")
	added := p.Ingest(&PeerExchange{From: "other", Peers: []PeerExchangeItem{
		{ID: "self", Addr: "x"},
	}})
	if added != 0 {
		t.Errorf("expected 0 added (self ignored), got %d", added)
	}
}

func TestPexIngestNil(t *testing.T) {
	p := NewPexSet("self")
	if n := p.Ingest(nil); n != 0 {
		t.Errorf("expected 0 for nil ingest, got %d", n)
	}
}

func TestPexBuildExchange(t *testing.T) {
	p := NewPexSet("self")
	p.Ingest(&PeerExchange{From: "a", Peers: []PeerExchangeItem{
		{ID: "node-1", Addr: "10.0.0.1:8000", Name: "one"},
		{ID: "node-2", Addr: "10.0.0.2:8000", Name: "two"},
	}})

	ex := p.BuildExchange("self", 10)
	if ex.From != "self" {
		t.Errorf("exchange From: got %q", ex.From)
	}
	if len(ex.Peers) != 2 {
		t.Errorf("exchange peers: got %d, want 2", len(ex.Peers))
	}
}

func TestPexBuildExchangeMaxLimit(t *testing.T) {
	p := NewPexSet("self")
	items := make([]PeerExchangeItem, 20)
	for i := range items {
		items[i] = PeerExchangeItem{
			ID:   fmt.Sprintf("node-%d", i),
			Addr: fmt.Sprintf("10.0.0.%d:8000", i),
		}
	}
	p.Ingest(&PeerExchange{From: "a", Peers: items})

	ex := p.BuildExchange("self", 5)
	if len(ex.Peers) != 5 {
		t.Errorf("exchange limited to 5: got %d", len(ex.Peers))
	}
}

func TestPexPickNewCandidates(t *testing.T) {
	p := NewPexSet("self")
	p.Ingest(&PeerExchange{From: "a", Peers: []PeerExchangeItem{
		{ID: "node-x", Addr: "x:1"},
		{ID: "node-y", Addr: "y:2"},
	}})

	cands := p.PickNewCandidates(1)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}

	// 再调一次 → 应该只返回未 tried 的
	remaining := p.PickNewCandidates(10)
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining candidate, got %d", len(remaining))
	}
}

func TestPexMarkTried(t *testing.T) {
	p := NewPexSet("self")
	p.AddDirectPeer("node-z", "10.0.0.5:8000", "zee")

	p.MarkTried("node-z")
	cands := p.PickNewCandidates(10)
	for _, c := range cands {
		if c.ID == "node-z" {
			t.Error("MarkTried node should not appear in candidates")
		}
	}
}

func TestPexAddDirectPeer(t *testing.T) {
	p := NewPexSet("self")
	p.AddDirectPeer("node-d", "10.0.0.4:8000", "direct")

	ex := p.BuildExchange("self", 10)
	found := false
	for _, item := range ex.Peers {
		if item.ID == "node-d" {
			found = true
			break
		}
	}
	if !found {
		t.Error("direct peer not found in exchange")
	}
}
