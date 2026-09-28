package decoy

import (
	"math/rand/v2"
	"testing"

	"github.com/s26059-maker/anchor/internal/gate"
)

func TestNewHandleNoDuplicates(t *testing.T) {
	m := NewMixer(rand.New(rand.NewPCG(1, 2)))
	seen := map[int]bool{}
	for i := 0; i < 5000; i++ {
		h := m.NewHandle()
		if seen[h] {
			t.Fatalf("핸들 중복: %d", h)
		}
		seen[h] = true
	}
}

func TestHandleFixedWithinRound(t *testing.T) {
	m := NewMixer(rand.New(rand.NewPCG(1, 2)))
	first := m.Handle(42)
	for i := 0; i < 10; i++ {
		if got := m.Handle(42); got != first {
			t.Fatalf("같은 라운드 안에서 핸들이 바뀜: 처음 %d, 이번 %d", first, got)
		}
	}
	other := m.Handle(43)
	if other == first {
		t.Fatalf("서로 다른 진짜 ID가 같은 핸들을 받음: %d", other)
	}
	if got := m.RealOf(first); got != 42 {
		t.Fatalf("RealOf(%d) = %d, 42를 기대", first, got)
	}
	if got := m.RealOf(other); got != 43 {
		t.Fatalf("RealOf(%d) = %d, 43을 기대", other, got)
	}
	if got := m.RealOf(999999); got != -1 {
		t.Fatalf("미끼 핸들의 RealOf는 -1이어야 하는데 %d", got)
	}
}

func TestMixShuffles(t *testing.T) {
	real := []gate.EnemyView{{ID: 1}, {ID: 2}, {ID: 3}}
	fakes := []gate.EnemyView{{ID: 100}, {ID: 101}, {ID: 102}}

	sameOrderAlways := true
	var firstOrder []int
	for trial := 0; trial < 50; trial++ {
		m := NewMixer(rand.New(rand.NewPCG(uint64(trial), 7)))
		out := m.Mix(real, fakes)
		if len(out) != len(real)+len(fakes) {
			t.Fatalf("Mix 결과 길이 = %d, %d를 기대", len(out), len(real)+len(fakes))
		}
		order := make([]int, len(out))
		for i, e := range out {
			order[i] = e.ID
		}
		if trial == 0 {
			firstOrder = order
			continue
		}
		if !sameSlice(order, firstOrder) {
			sameOrderAlways = false
		}
	}
	if sameOrderAlways {
		t.Fatalf("Mix가 매번 같은 순서를 내놓음 (섞이지 않음)")
	}
}

func sameSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
