// Package gate: 가시성 게이트(정보의 닻).
// 서버가 viewer에게 보낼 적을 고른다. 안 보내면 메모리에 없고, 메모리에 없으면 월핵이 읽을 것도 없다.
package gate

import (
	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/world"
)

type Gate struct {
	Enabled   bool
	Lookahead float64 // 선공개 구간(초). 네트워크 지연보다 짧으면 정직한 사람 화면에 적이 늦게 튀어나온다.
	Samples   int     // 선공개 구간을 몇 점으로 나눠 예측할지
}

// Snapshot: 한 viewer의 클라이언트가 받는 적 정보.
type Snapshot struct {
	Time    float64
	Enemies []EnemyView
}

type EnemyView struct {
	ID  int
	Pos geom.Vec
}

// Stats: 안 보이는 적이 새어 나간 비율.
type Stats struct {
	HiddenTicks int // 적이 실제로 안 보이던 (viewer, 적, 틱) 수
	LeakedTicks int // 그중 그래도 전송된 수
}

func (s Stats) LeakRatio() float64 {
	if s.HiddenTicks == 0 {
		return 0
	}
	return float64(s.LeakedTicks) / float64(s.HiddenTicks)
}

func (g *Gate) Filter(w *world.World, viewer *world.Player, st *Stats) Snapshot {
	snap := Snapshot{Time: w.Time}
	for _, e := range w.Enemies(viewer) {
		visible := w.Map.LOS(viewer.Pos, e.Pos)
		send := !g.Enabled || visible || g.soonVisible(w, viewer, e)
		if !visible && st != nil {
			st.HiddenTicks++
			if send {
				st.LeakedTicks++
			}
		}
		if send {
			snap.Enemies = append(snap.Enemies, EnemyView{ID: e.ID, Pos: e.Pos})
		}
	}
	return snap
}

// soonVisible: 둘 다 지금 속도로 Lookahead초 움직였을 때 한 번이라도 보이면 true.
func (g *Gate) soonVisible(w *world.World, v, e *world.Player) bool {
	if g.Lookahead <= 0 || g.Samples <= 0 {
		return false
	}
	for k := 1; k <= g.Samples; k++ {
		t := g.Lookahead * float64(k) / float64(g.Samples)
		if w.Map.LOS(v.Pos.Add(v.Vel.Scale(t)), e.Pos.Add(e.Vel.Scale(t))) {
			return true
		}
	}
	return false
}
