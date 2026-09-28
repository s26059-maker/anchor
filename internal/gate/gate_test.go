package gate

import (
	"testing"

	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/world"
)

func wallWorld(enemyPos, enemyVel geom.Vec) (*world.World, *world.Player, *world.Player) {
	m := &world.Map{Walls: []geom.Segment{{A: geom.Vec{X: 0, Y: -6}, B: geom.Vec{X: 0, Y: 6}}}}
	viewer := &world.Player{ID: 1, Team: 0, Pos: geom.Vec{X: 20, Y: 0}, Alive: true}
	enemy := &world.Player{ID: 2, Team: 1, Pos: enemyPos, Vel: enemyVel, Alive: true}
	w := &world.World{Map: m, Players: []*world.Player{viewer, enemy}}
	return w, viewer, enemy
}

func TestVisibleAlwaysSent(t *testing.T) {
	// 벽을 가로지르지 않는 위치: 항상 보인다.
	w, viewer, _ := wallWorld(geom.Vec{X: 25, Y: 0}, geom.Vec{})
	for _, enabled := range []bool{false, true} {
		g := Gate{Enabled: enabled, Lookahead: 0.02, Samples: 8}
		snap := g.Filter(w, viewer, nil)
		if len(snap.Enemies) != 1 {
			t.Fatalf("게이트 enabled=%v: 보이는 적이 전송되지 않음", enabled)
		}
	}
}

func TestHiddenOutsideLookaheadNeverSent(t *testing.T) {
	// 벽 뒤에 가만히 있고, 그 어떤 미래 시점에도(속도 0) 보이지 않는다.
	w, viewer, _ := wallWorld(geom.Vec{X: -1.5, Y: 0}, geom.Vec{})
	var st Stats
	g := Gate{Enabled: true, Lookahead: 0.02, Samples: 8}
	for i := 0; i < 1000; i++ {
		snap := g.Filter(w, viewer, &st)
		if len(snap.Enemies) != 0 {
			t.Fatalf("선공개 밖에서 숨은 적이 전송됨: %v", snap.Enemies)
		}
	}
	if st.HiddenTicks == 0 {
		t.Fatalf("HiddenTicks가 0 — 테스트 자체가 숨은 상황을 만들지 못함")
	}
	if st.LeakedTicks != 0 {
		t.Fatalf("LeakedTicks = %d, 0을 기대", st.LeakedTicks)
	}
	if st.LeakRatio() != 0 {
		t.Fatalf("LeakRatio() = %v, 0을 기대", st.LeakRatio())
	}
}

func TestSoonVisibleSentWithinLookahead(t *testing.T) {
	// 벽 뒤에 있지만 Lookahead 안에 보일 곳으로 다가가는 적: soonVisible이 잡아서 보내야 한다.
	w, viewer, _ := wallWorld(geom.Vec{X: -1.5, Y: 6.3}, geom.Vec{X: 0, Y: 5})
	g := Gate{Enabled: true, Lookahead: 0.1, Samples: 8}
	snap := g.Filter(w, viewer, nil)
	if len(snap.Enemies) != 1 {
		t.Fatalf("선공개 구간 안의 적이 전송되지 않음")
	}
}

func TestGateOffSendsEverything(t *testing.T) {
	w, viewer, _ := wallWorld(geom.Vec{X: -1.5, Y: 0}, geom.Vec{})
	g := Gate{Enabled: false}
	snap := g.Filter(w, viewer, nil)
	if len(snap.Enemies) != 1 {
		t.Fatalf("게이트 꺼짐: 숨은 적도 전송돼야 하는데 안 됨")
	}
}

// ViewerOnly: 적 스스로의 움직임으로 보이게 되는 경우는 절대 선공개하지 않는다(viewer가 안 움직이면).
func TestViewerOnlyIgnoresEnemyMotion(t *testing.T) {
	w, viewer, _ := wallWorld(geom.Vec{X: -1.5, Y: 6.3}, geom.Vec{X: 0, Y: 5}) // 적이 다가온다, viewer는 고정
	g := Gate{Enabled: true, Mode: ViewerOnly, Lookahead: 0.1, Samples: 8}
	snap := g.Filter(w, viewer, nil)
	if len(snap.Enemies) != 0 {
		t.Fatalf("ViewerOnly: 적 자신의 움직임만으로 선공개됨(뜨면 안 됨)")
	}
	// 같은 상황, Both 모드라면 (기존 동작으로) 전송돼야 한다 — 대조 확인.
	gBoth := Gate{Enabled: true, Mode: Both, Lookahead: 0.1, Samples: 8}
	snapBoth := gBoth.Filter(w, viewer, nil)
	if len(snapBoth.Enemies) != 1 {
		t.Fatalf("대조군(Both) 실패: 적의 움직임으로 선공개돼야 하는데 안 됨")
	}
}

// ViewerOnly: viewer 자신의 움직임으로 보이게 되는 경우는 여전히 선공개한다(팝인 방지).
func TestViewerOnlyStillPredictsViewerMotion(t *testing.T) {
	m := &world.Map{Walls: []geom.Segment{{A: geom.Vec{X: 0, Y: -6}, B: geom.Vec{X: 0, Y: 6}}}}
	viewer := &world.Player{ID: 1, Team: 0, Pos: geom.Vec{X: -1.5, Y: 6.3}, Vel: geom.Vec{X: 0, Y: 5}, Alive: true}
	enemy := &world.Player{ID: 2, Team: 1, Pos: geom.Vec{X: 20, Y: 0}, Alive: true} // 고정
	w := &world.World{Map: m, Players: []*world.Player{viewer, enemy}}

	g := Gate{Enabled: true, Mode: ViewerOnly, Lookahead: 0.1, Samples: 8}
	snap := g.Filter(w, viewer, nil)
	if len(snap.Enemies) != 1 {
		t.Fatalf("ViewerOnly: viewer 자신의 움직임으로는 여전히 선공개돼야 하는데 안 됨")
	}
}
