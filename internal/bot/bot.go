// Package bot: 사람 한 명의 반응 모델.
// 정직한 플레이어와 월핵 유저는 뇌가 똑같다. 다른 건 딱 하나, 화면에 안 그려진 적(메모리 데이터)을 쓰느냐다.
package bot

import (
	"math"
	"math/rand/v2"

	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/world"
)

type Human struct {
	Rng        *rand.Rand
	UsesHidden bool      // true = 월핵. 벽 뒤 적 데이터도 본다
	Trust      float64   // 월핵이 벽 뒤 신호 하나를 믿고 움직일 확률 (1 = 전부 믿음)
	Reaction   float64   // 적을 처음 인지하고 손이 움직이기까지(초)
	Omega      float64   // 에임 회전 속도(도/초)
	Settle     float64   // 조준 완료 후 방아쇠까지(초)
	Anticipate float64   // 적이 화면에 실제로 나타난 뒤 최소 대기(초)
	Tol        float64   // 조준 완료로 보는 오차(도)
	Forget     float64   // 목표가 이만큼 안 보이면 포기(초)
	Holds      []float64 // 할 일 없을 때 번갈아 지키는 각도들 (nil이면 가만히)
	HoldEvery  float64   // 평균 몇 초마다 지키는 각도를 바꾸나
	Aim        float64   // 현재 조준 방향(도)

	started    bool
	target     int
	lastPos    geom.Vec
	reactDone  float64
	onScreenAt float64
	aimedAt    float64
	missing    float64
	hold       float64
	ignored    map[int]bool
}

func (h *Human) init() {
	h.started = true
	h.target, h.onScreenAt, h.aimedAt = -1, -1, -1
	h.hold = h.Aim
	h.ignored = map[int]bool{}
}

func (h *Human) turn(want, dt float64) {
	d := geom.AngleDiff(h.Aim, want)
	step := h.Omega * dt
	if math.Abs(d) <= step {
		h.Aim = want
	} else {
		h.Aim += math.Copysign(step, d)
	}
}

// Update: 이번 틱에 쏘면 (true, 목표 ID)를 돌려준다.
func (h *Human) Update(now, dt float64, self *world.Player, view gate.Snapshot, m *world.Map) (bool, int) {
	if !h.started {
		h.init()
	}
	// 1. 이 사람이 "아는" 적: 화면에 보이는 적 + (월핵이면) 믿기로 한 벽 뒤 신호
	var cand []gate.EnemyView
	for _, e := range view.Enemies {
		if m.LOS(self.Pos, e.Pos) {
			cand = append(cand, e)
			continue
		}
		if !h.UsesHidden || h.ignored[e.ID] {
			continue
		}
		if e.ID != h.target && h.Rng.Float64() >= h.Trust {
			h.ignored[e.ID] = true // 이 신호는 안 믿기로 함
			continue
		}
		cand = append(cand, e)
	}
	if h.target < 0 && len(cand) > 0 {
		best := cand[0]
		for _, e := range cand[1:] {
			if e.Pos.Sub(self.Pos).Len() < best.Pos.Sub(self.Pos).Len() {
				best = e
			}
		}
		h.target = best.ID
		h.reactDone = now + h.Reaction
		h.onScreenAt, h.aimedAt, h.missing = -1, -1, 0
	}
	if h.target < 0 {
		if len(h.Holds) > 0 && h.HoldEvery > 0 {
			if h.Rng.Float64() < dt/h.HoldEvery {
				h.hold = h.Holds[h.Rng.IntN(len(h.Holds))]
			}
			h.turn(h.hold, dt)
		}
		return false, -1
	}

	// 2. 목표 위치 갱신. 오래 안 보이면 포기 (미끼였을 수도)
	found := false
	for _, e := range cand {
		if e.ID == h.target {
			h.lastPos, found = e.Pos, true
		}
	}
	if !found {
		h.missing += dt
		if h.Forget > 0 && h.missing > h.Forget {
			h.target = -1
			return false, -1
		}
	} else {
		h.missing = 0
	}
	onScreen := found && m.LOS(self.Pos, h.lastPos)
	if onScreen && h.onScreenAt < 0 {
		h.onScreenAt = now
	}

	// 3. 반응이 끝났으면 에임을 돌린다
	want := self.Pos.AngleTo(h.lastPos)
	if now >= h.reactDone {
		h.turn(want, dt)
	}
	if math.Abs(geom.AngleDiff(h.Aim, want)) <= h.Tol {
		if h.aimedAt < 0 {
			h.aimedAt = now
		}
	} else {
		h.aimedAt = -1
	}

	// 4. 방아쇠
	fire := onScreen && h.aimedAt >= 0 &&
		now >= h.aimedAt+h.Settle &&
		now >= h.onScreenAt+h.Anticipate
	return fire, h.target
}
