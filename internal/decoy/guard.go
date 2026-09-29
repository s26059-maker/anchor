package decoy

import (
	"math"

	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/world"
)

// Guard: 미끼 안전 불변식.
//
//	미끼를 보내기 전에, 클라이언트가 받은 위치 + 속도×t 로 미끼를 그려도(0 ≤ t ≤ MaxExtrap)
//	viewer와 LOS가 한 번도 생기지 않는지 검사한다. 어기는 미끼는 보내지 않는다.
//
// LOS가 생기면 정직한 클라이언트 화면에 유령이 뜬다. 2.6단계에서 외삽 클라이언트가 거울 미끼를
// 진짜로 착각해 공정성이 무너진 것이 바로 이 경우다.
//
// viewer의 움직임은 세 가지로 본다: 제자리, 지금 속도 그대로, 그리고 (ViewerMaxSpeed > 0이면)
// 최대 속력으로 Dirs개 방향 아무 데로나. 서버는 viewer가 다음 순간 방향을 바꿀지 모르기 때문이다.
type Guard struct {
	Map            *world.Map
	MaxExtrap      float64 // 클라이언트 최대 외삽 시간(초)
	ViewerMaxSpeed float64 // 0이면 viewer는 제자리/현재 속도만 본다
	Dirs           int     // 최대 속력 방향 수 (기본 16)
	StepLen        float64 // 검사 간격을 이 거리(m)만큼 움직이는 시간으로 잡는다 (기본 0.01)

	Checked int // 검사한 미끼 수
	Dropped int // 불변식을 어겨 거둔 미끼 수
}

// Safe: 불변식이 지켜지면 true.
func (g *Guard) Safe(viewer *world.Player, f gate.EnemyView) bool {
	vels := []geom.Vec{{}, viewer.Vel}
	if g.ViewerMaxSpeed > 0 {
		n := g.Dirs
		if n <= 0 {
			n = 16
		}
		for i := 0; i < n; i++ {
			a := 2 * math.Pi * float64(i) / float64(n)
			vels = append(vels, geom.Vec{X: math.Cos(a) * g.ViewerMaxSpeed, Y: math.Sin(a) * g.ViewerMaxSpeed})
		}
	}
	return g.safeAgainst(viewer.Pos, vels, f)
}

func (g *Guard) safeAgainst(vp geom.Vec, vels []geom.Vec, f gate.EnemyView) bool {
	maxRel := f.Vel.Len() + g.ViewerMaxSpeed
	for _, v := range vels {
		maxRel = math.Max(maxRel, f.Vel.Sub(v).Len())
	}
	stepLen := g.StepLen
	if stepLen <= 0 {
		stepLen = 0.01
	}
	step := g.MaxExtrap
	if maxRel > 0 {
		step = math.Min(step, stepLen/maxRel)
	}
	if step <= 0 {
		step = 1e-3
	}
	n := int(math.Ceil(g.MaxExtrap / step))
	for k := 0; k <= n; k++ {
		t := math.Min(float64(k)*step, g.MaxExtrap)
		fp := f.Pos.Add(f.Vel.Scale(t))
		for _, v := range vels {
			if g.Map.LOS(vp.Add(v.Scale(t)), fp) {
				return false
			}
		}
	}
	return true
}

// Filter: 불변식을 지키는 미끼만 남긴다.
func (g *Guard) Filter(viewer *world.Player, fakes []gate.EnemyView) []gate.EnemyView {
	out := fakes[:0:0]
	for _, f := range fakes {
		g.Checked++
		if g.Safe(viewer, f) {
			out = append(out, f)
		} else {
			g.Dropped++
		}
	}
	return out
}
