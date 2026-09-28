// Package world: 서버만 가진 진짜 게임 상태. 클라이언트는 이걸 절대 통째로 받지 않는다.
package world

import "github.com/s26059-maker/anchor/internal/geom"

type Map struct{ Walls []geom.Segment }

// LOS: a에서 b까지 벽에 안 막히면 true.
func (m *Map) LOS(a, b geom.Vec) bool {
	for _, w := range m.Walls {
		if geom.Intersects(a, b, w) {
			return false
		}
	}
	return true
}

type Player struct {
	ID    int
	Team  int
	Pos   geom.Vec
	Vel   geom.Vec // 초당 이동량. 게이트의 선공개 예측에 쓴다.
	Alive bool
}

type World struct {
	Map     *Map
	Players []*Player
	Time    float64 // 초
}

// Enemies: viewer 기준 살아 있는 적.
func (w *World) Enemies(viewer *Player) []*Player {
	var out []*Player
	for _, p := range w.Players {
		if p.Team != viewer.Team && p.Alive {
			out = append(out, p)
		}
	}
	return out
}

func (w *World) ByID(id int) *Player {
	for _, p := range w.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (w *World) Step(dt float64) {
	for _, p := range w.Players {
		if p.Alive {
			p.Pos = p.Pos.Add(p.Vel.Scale(dt))
		}
	}
	w.Time += dt
}
