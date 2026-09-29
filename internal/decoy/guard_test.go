package decoy

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/world"
)

func wallMap() *world.Map {
	return &world.Map{Walls: []geom.Segment{{A: geom.Vec{X: 0, Y: -6}, B: geom.Vec{X: 0, Y: 6}}}}
}

func randMap(r *rand.Rand) *world.Map {
	m := &world.Map{}
	for i, n := 0, 5+r.IntN(5); i < n; i++ {
		a := geom.Vec{X: 40*r.Float64() - 20, Y: 40*r.Float64() - 20}
		b := a.Add(geom.Vec{X: 16*r.Float64() - 8, Y: 16*r.Float64() - 8})
		m.Walls = append(m.Walls, geom.Segment{A: a, B: b})
	}
	return m
}

// oracle: 불변식을 아주 촘촘하게(0.05mm 단위) 훑는 독립 구현. 위반이 있으면 true.
func oracleViolates(m *world.Map, vp, vv geom.Vec, f gate.EnemyView, maxExtrap float64) bool {
	rel := f.Vel.Sub(vv).Len()
	step := 5e-5
	if rel > 0 {
		step = math.Min(step/rel, maxExtrap/10)
	}
	for t := 0.0; t <= maxExtrap+1e-12; t += step {
		if m.LOS(vp.Add(vv.Scale(t)), f.Pos.Add(f.Vel.Scale(t))) {
			return true
		}
	}
	return false
}

// 속성: 지금 속도(와 제자리)로 그리는 경우 — Safe면 oracle이 LOS를 못 찾아야 한다.
func TestGuardPropertyCurrentVelocity(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	safe, unsafe := 0, 0
	for i := 0; i < 3000; i++ {
		m := randMap(r)
		g := &Guard{Map: m, MaxExtrap: 0.05 + 0.2*r.Float64()}
		viewer := &world.Player{Pos: geom.Vec{X: 40*r.Float64() - 20, Y: 40*r.Float64() - 20},
			Vel: geom.Vec{X: 10*r.Float64() - 5, Y: 10*r.Float64() - 5}}
		f := gate.EnemyView{Pos: geom.Vec{X: 40*r.Float64() - 20, Y: 40*r.Float64() - 20},
			Vel: geom.Vec{X: 20*r.Float64() - 10, Y: 20*r.Float64() - 10}}
		if g.Safe(viewer, f) {
			safe++
			for _, vv := range []geom.Vec{{}, viewer.Vel} {
				if oracleViolates(m, viewer.Pos, vv, f, g.MaxExtrap) {
					t.Fatalf("Safe인데 LOS 발생: 맵=%v viewer=%+v vv=%v 미끼=%+v T=%v", m.Walls, viewer, vv, f, g.MaxExtrap)
				}
			}
		} else {
			unsafe++
		}
	}
	if safe < 300 || unsafe < 300 {
		t.Fatalf("속성 테스트가 한쪽으로 쏠림: safe=%d unsafe=%d", safe, unsafe)
	}
}

// 속성: 최대 속력 방향 전부를 볼 때 — 검사한 방향들에 대해 oracle이 LOS를 못 찾아야 한다.
func TestGuardPropertyWorstCaseViewer(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 5))
	safe, unsafe := 0, 0
	for i := 0; i < 2000; i++ {
		m := randMap(r)
		g := &Guard{Map: m, MaxExtrap: 0.05 + 0.15*r.Float64(), ViewerMaxSpeed: 5, Dirs: 8}
		viewer := &world.Player{Pos: geom.Vec{X: 40*r.Float64() - 20, Y: 40*r.Float64() - 20},
			Vel: geom.Vec{X: 10*r.Float64() - 5, Y: 10*r.Float64() - 5}}
		f := gate.EnemyView{Pos: geom.Vec{X: 40*r.Float64() - 20, Y: 40*r.Float64() - 20},
			Vel: geom.Vec{X: 20*r.Float64() - 10, Y: 20*r.Float64() - 10}}
		if !g.Safe(viewer, f) {
			unsafe++
			continue
		}
		safe++
		vels := []geom.Vec{{}, viewer.Vel}
		for k := 0; k < 8; k++ {
			a := 2 * math.Pi * float64(k) / 8
			vels = append(vels, geom.Vec{X: 5 * math.Cos(a), Y: 5 * math.Sin(a)})
		}
		for _, vv := range vels {
			if oracleViolates(m, viewer.Pos, vv, f, g.MaxExtrap) {
				t.Fatalf("Safe인데 LOS 발생: viewer=%+v vv=%v 미끼=%+v", viewer, vv, f)
			}
		}
	}
	if safe < 100 || unsafe < 100 {
		t.Fatalf("속성 테스트가 한쪽으로 쏠림: safe=%d unsafe=%d", safe, unsafe)
	}
}

// 반대 방향: 확실히 LOS가 생기는 미끼는 거둬야 한다(불변식이 공허하지 않다).
func TestGuardRejectsObviousExposure(t *testing.T) {
	g := &Guard{Map: wallMap(), MaxExtrap: 0.1}
	viewer := &world.Player{Pos: geom.Vec{X: 20, Y: 0}}
	// 이미 벽 밖 (LOS 있음)
	if g.Safe(viewer, gate.EnemyView{Pos: geom.Vec{X: -1.5, Y: 7}}) {
		t.Fatal("이미 보이는 미끼를 통과시킴")
	}
	// 벽 뒤에서 출구를 향해 달려 0.1초 안에 노출: y=6.2 → 6.7 (출구 6.45)
	if g.Safe(viewer, gate.EnemyView{Pos: geom.Vec{X: -1.5, Y: 6.2}, Vel: geom.Vec{Y: 5}}) {
		t.Fatal("외삽하면 노출되는 미끼를 통과시킴")
	}
	// 같은 위치라도 속도 0이면 안전
	if !g.Safe(viewer, gate.EnemyView{Pos: geom.Vec{X: -1.5, Y: 6.2}}) {
		t.Fatal("안전한 미끼를 거둠")
	}
	out := g.Filter(viewer, []gate.EnemyView{
		{ID: 1, Pos: geom.Vec{X: -1.5, Y: 6.2}, Vel: geom.Vec{Y: 5}},
		{ID: 2, Pos: geom.Vec{X: -1.5, Y: 0}},
	})
	if len(out) != 1 || out[0].ID != 2 || g.Dropped != 1 || g.Checked != 2 {
		t.Fatalf("Filter 결과 이상: %+v dropped=%d checked=%d", out, g.Dropped, g.Checked)
	}
}

// viewer가 움직이면 노출이 생기는 경우: 지금은 제자리여도 최대 속력을 보면 거둔다.
func TestGuardViewerWorstCase(t *testing.T) {
	m := wallMap()
	viewer := &world.Player{Pos: geom.Vec{X: -1.5, Y: 5.9}} // 벽 끝 근처, 제자리
	fake := gate.EnemyView{Pos: geom.Vec{X: 20, Y: 0}}      // 벽 반대편에 가만히
	loose := &Guard{Map: m, MaxExtrap: 0.2}
	strict := &Guard{Map: m, MaxExtrap: 0.2, ViewerMaxSpeed: 5}
	if !loose.Safe(viewer, fake) {
		t.Fatal("viewer가 제자리면 안전해야 한다")
	}
	if strict.Safe(viewer, fake) {
		t.Fatal("viewer가 최대 속력으로 뛰면 노출되는 미끼를 통과시킴")
	}
}
