// Package decoy: 선공개 구간의 정보를 믿을 수 없게 만든다.
//
// 핵심 규칙 두 가지.
//  1. 진짜 적의 ID를 그대로 보내지 않는다. 라운드마다 새 핸들로 바꾼다.
//     ID가 고정이면 치트는 "처음 보는 ID = 미끼"로 바로 걸러낸다.
//  2. 진짜와 미끼를 섞어서(순서까지 섞어서) 보낸다. 목록 순서도 정보다.
package decoy

import (
	"math/rand/v2"

	"github.com/s26059-maker/anchor/internal/gate"
)

type Mixer struct {
	rng     *rand.Rand
	handles map[int]int
	used    map[int]bool
}

func NewMixer(r *rand.Rand) *Mixer {
	return &Mixer{rng: r, handles: map[int]int{}, used: map[int]bool{}}
}

// NewHandle: 겹치지 않는 무작위 핸들.
func (m *Mixer) NewHandle() int {
	for {
		h := 1000 + m.rng.IntN(1_000_000)
		if !m.used[h] {
			m.used[h] = true
			return h
		}
	}
}

// Handle: 진짜 적 ID → 이번 라운드용 핸들 (라운드 안에서는 고정).
func (m *Mixer) Handle(realID int) int {
	h, ok := m.handles[realID]
	if !ok {
		h = m.NewHandle()
		m.handles[realID] = h
	}
	return h
}

// Mix: 진짜 적 ID를 핸들로 바꾸고, 미끼를 섞고, 순서를 섞는다.
func (m *Mixer) Mix(real []gate.EnemyView, fakes []gate.EnemyView) []gate.EnemyView {
	out := make([]gate.EnemyView, 0, len(real)+len(fakes))
	for _, e := range real {
		out = append(out, gate.EnemyView{ID: m.Handle(e.ID), Pos: e.Pos})
	}
	out = append(out, fakes...)
	m.rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// RealOf: 핸들 → 진짜 ID (서버 판정용). 미끼면 -1.
func (m *Mixer) RealOf(handle int) int {
	for id, h := range m.handles {
		if h == handle {
			return id
		}
	}
	return -1
}
