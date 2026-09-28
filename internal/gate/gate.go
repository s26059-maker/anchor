// Package gate: 가시성 게이트(정보의 닻).
// 서버가 viewer에게 보낼 적을 고른다. 안 보내면 메모리에 없고, 메모리에 없으면 월핵이 읽을 것도 없다.
package gate

import (
	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/world"
)

// Mode: soonVisible이 누구의 미래 이동을 예측에 쓰는가.
type Mode int

const (
	// Both: viewer와 적 둘 다 지금 속도로 미래 위치를 예측한다 (기존 동작).
	// 적이 스스로 움직여서 보이게 되는 경우까지 선공개하므로, 그 구간의 정보를 월핵이 읽을 수 있다.
	Both Mode = iota
	// ViewerOnly: viewer 자신의 이동만 예측하고, 적은 지금 위치에 고정해서 본다.
	// "내가 움직여서 보이게 되는" 경우의 팝인만 막고, "적이 움직여서 보이게 되는" 경우는
	// 선공개하지 않는다 — 후자는 정직한 클라이언트에게 이득이 없고 월핵에게만 이득이라는 가설.
	ViewerOnly
)

type Gate struct {
	Enabled   bool
	Mode      Mode    // 기본값(Both)은 기존 동작과 같다
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
	Vel geom.Vec // 클라이언트가 외삽(dead reckoning)으로 렌더링할 때 쓴다
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
			snap.Enemies = append(snap.Enemies, EnemyView{ID: e.ID, Pos: e.Pos, Vel: e.Vel})
		}
	}
	return snap
}

// soonVisible: Lookahead초 안에 한 번이라도 보이면 true.
// Mode가 Both면 viewer와 적 둘 다 지금 속도로 움직인다고 예측한다.
// Mode가 ViewerOnly면 viewer만 움직인다고 보고 적은 지금 위치에 고정한다.
func (g *Gate) soonVisible(w *world.World, v, e *world.Player) bool {
	if g.Lookahead <= 0 || g.Samples <= 0 {
		return false
	}
	eVel := e.Vel
	if g.Mode == ViewerOnly {
		eVel = geom.Vec{}
	}
	for k := 1; k <= g.Samples; k++ {
		t := g.Lookahead * float64(k) / float64(g.Samples)
		if w.Map.LOS(v.Pos.Add(v.Vel.Scale(t)), e.Pos.Add(eVel.Scale(t))) {
			return true
		}
	}
	return false
}
