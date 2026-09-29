package netsim

import (
	"math"
	"testing"

	"github.com/s26059-maker/anchor/internal/gate"
)

func TestLinkArrivalMonotone(t *testing.T) {
	// 지터가 평균보다 크고 스파이크도 있어도 도착 시각은 엄격히 증가한다.
	l := NewLink(Model{Mean: 0.02, Jitter: 0.03, SpikeProb: 0.05, SpikeSize: 0.2}, 1, 2)
	prev := math.Inf(-1)
	for i := 0; i < 100000; i++ {
		a := l.Arrive(float64(i) / 128)
		if a <= prev {
			t.Fatalf("도착 시각이 역전/정체: %d번째 %v <= %v", i, a, prev)
		}
		prev = a
	}
}

func TestLinkStatistics(t *testing.T) {
	// 지터만 있을 때(순서 역전이 거의 없는 작은 지터) 평균·표준편차가 모델과 맞는다.
	l := NewLink(Model{Mean: 0.02, Jitter: 0.002}, 5, 6)
	var xs []float64
	for i := 0; i < 50000; i++ {
		ts := float64(i) * 0.1 // 간격을 크게 잡아 단조 보정이 안 걸리게
		xs = append(xs, l.Arrive(ts)-ts)
	}
	mean, sd := meanSD(xs)
	if math.Abs(mean-0.02) > 1e-4 || math.Abs(sd-0.002) > 1e-4 {
		t.Fatalf("평균 %.5f sd %.5f, 기대 0.02 / 0.002", mean, sd)
	}
	// 스파이크: 비율 확인
	l = NewLink(Model{Mean: 0.02, SpikeProb: 0.1, SpikeSize: 0.1}, 5, 7)
	spikes := 0
	for i := 0; i < 50000; i++ {
		ts := float64(i) * 1.0
		if l.Arrive(ts)-ts > 0.05 {
			spikes++
		}
	}
	if r := float64(spikes) / 50000; math.Abs(r-0.1) > 0.01 {
		t.Fatalf("스파이크 비율 %.3f, 기대 0.1", r)
	}
}

func meanSD(xs []float64) (float64, float64) {
	m := 0.0
	for _, x := range xs {
		m += x
	}
	m /= float64(len(xs))
	v := 0.0
	for _, x := range xs {
		v += (x - m) * (x - m)
	}
	return m, math.Sqrt(v / float64(len(xs)))
}

func TestConstantDelayUnchanged(t *testing.T) {
	// Down이 없으면 기존 동작: Time+Delay 이후에만 보인다.
	d := &DelayLine{Delay: 0.02}
	d.Push(gate.Snapshot{Time: 1.0, Enemies: []gate.EnemyView{{ID: 1}}})
	if got := d.View(1.019); len(got.Enemies) != 0 {
		t.Fatal("지연이 지나기 전에 보임")
	}
	if got := d.View(1.02); len(got.Enemies) != 1 {
		t.Fatal("지연이 지났는데 안 보임")
	}
}

func TestMeterKnowsOnlyAckedSamples(t *testing.T) {
	m := &Meter{Window: 4, Initial: 0.05}
	if got := m.Estimate(0); got != 0.05 {
		t.Fatalf("샘플이 없으면 초기값: %v", got)
	}
	m.add(1.0, 0.030)
	m.add(2.0, 0.010)
	if got := m.Estimate(0.5); got != 0.05 {
		t.Fatalf("ack가 도착하기 전엔 모른다: %v", got)
	}
	if got := m.Estimate(1.5); math.Abs(got-0.030) > 1e-12 {
		t.Fatalf("첫 샘플만: %v", got)
	}
	if got := m.Estimate(2.5); math.Abs(got-0.020) > 1e-12 {
		t.Fatalf("두 샘플 평균: %v", got)
	}
	for i := 0; i < 10; i++ {
		m.add(3.0+float64(i), 0.100)
	}
	if got := m.Estimate(100); math.Abs(got-0.100) > 1e-12 {
		t.Fatalf("창 4개면 최근 것만: %v", got)
	}
}

func TestMeterTracksLinkMean(t *testing.T) {
	d := &DelayLine{Down: NewLink(Model{Mean: 0.03, Jitter: 0.004}, 9, 1), Up: NewLink(Model{Mean: 0.03, Jitter: 0.004}, 9, 2),
		Meter: &Meter{Window: 32, Initial: 0.0}}
	var est float64
	for i := 0; i < 2000; i++ {
		ts := float64(i) / 128
		d.Push(gate.Snapshot{Time: ts})
		est = d.Meter.Estimate(ts)
	}
	if math.Abs(est-0.03) > 0.004 {
		t.Fatalf("추정 %.4f, 기대 ≈0.03", est)
	}
}
