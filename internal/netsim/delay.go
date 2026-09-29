// Package netsim: 서버 스냅샷이 클라이언트 화면에 뜨기까지의 지연을 흉내 낸다.
package netsim

import (
	"math"
	"math/rand/v2"
	"sort"

	"github.com/s26059-maker/anchor/internal/gate"
)

// Model: 한 플레이어의 편도 지연 분포. 지연 = 평균 + 정규 지터 + (확률적) 스파이크.
type Model struct {
	Mean      float64 // 평균 편도 지연(초)
	Jitter    float64 // 정규 지터 표준편차(초)
	SpikeProb float64 // 스냅샷마다 스파이크가 붙을 확률
	SpikeSize float64 // 스파이크 크기(초)
}

// MinDelay: 뽑은 지연의 하한(초). 음수 지연은 없다.
const MinDelay = 0.001

// Link: 한 방향 링크. 도착 시각이 단조 증가하도록 순서 역전을 막는다
// (늦은 패킷 뒤의 패킷은 앞 패킷이 도착한 뒤에야 도착한다 — 큐 앞 막힘).
type Link struct {
	Model Model
	rng   *rand.Rand
	last  float64
}

func NewLink(m Model, seed, stream uint64) *Link {
	return &Link{Model: m, rng: rand.New(rand.NewPCG(seed, stream*0x9e3779b97f4a7c15+1)), last: math.Inf(-1)}
}

// Arrive: sendTime에 보낸 패킷의 도착 시각. 호출 순서대로 단조 증가한다.
func (l *Link) Arrive(sendTime float64) float64 {
	d := l.Model.Mean
	if l.Model.Jitter > 0 {
		d += l.Model.Jitter * l.rng.NormFloat64()
	}
	if l.Model.SpikeProb > 0 && l.rng.Float64() < l.Model.SpikeProb {
		d += l.Model.SpikeSize
	}
	if d < MinDelay {
		d = MinDelay
	}
	a := sendTime + d
	if a <= l.last {
		a = l.last + 1e-6
	}
	l.last = a
	return a
}

// Meter: 서버가 아는 것은 실제 지연이 아니라 ack로 잰 값뿐이다.
// 스냅샷 i의 측정값 = (하행 지연 + 상행 지연)/2 이고, ack가 서버에 도착한 뒤에야 알 수 있다.
// 추정치 = 알려진 것 중 최근 Window개의 평균 (하나도 없으면 Initial, 접속 때 잰 핑).
type Meter struct {
	Window  int
	Initial float64
	// Quantile: 0이면 평균(기존 동작), (0,1]이면 그 분위수(예: 0.95)를 추정치로 쓴다.
	Quantile float64
	// SpikeGap: >0이면 알려진 중앙값보다 이만큼(초) 넘게 늦은 샘플은 스파이크로 보고 추정에 넣지 않는다.
	// 스파이크 때는 선공개 여유를 늘리지 않고 팝인을 받아들인다는 뜻이다.
	SpikeGap float64
	Spikes   int // 걸러낸 스파이크 샘플 수
	pending  []sample
	known    []float64
}

type sample struct{ at, half float64 }

func (m *Meter) add(ackAt, half float64) { m.pending = append(m.pending, sample{ackAt, half}) }

// Estimate: now 시점 서버의 편도 지연 추정치(초).
func (m *Meter) Estimate(now float64) float64 {
	// ack 도착은 순서가 뒤바뀔 수 있어 pending 전체를 훑는다(길이는 왕복 지연 × 틱률 정도로 짧다).
	keep := m.pending[:0]
	for _, s := range m.pending {
		if s.at <= now {
			if m.SpikeGap > 0 && len(m.known) >= 4 && s.half > median(m.known)+m.SpikeGap {
				m.Spikes++
				continue
			}
			m.known = append(m.known, s.half)
		} else {
			keep = append(keep, s)
		}
	}
	m.pending = keep
	w := m.Window
	if w <= 0 {
		w = 16
	}
	if len(m.known) > w {
		m.known = m.known[len(m.known)-w:]
	}
	if len(m.known) == 0 {
		return m.Initial
	}
	if m.Quantile > 0 {
		c := append([]float64(nil), m.known...)
		sort.Float64s(c)
		i := int(math.Ceil(m.Quantile*float64(len(c)))) - 1
		return c[min(max(i, 0), len(c)-1)]
	}
	sum := 0.0
	for _, v := range m.known {
		sum += v
	}
	return sum / float64(len(m.known))
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	return c[len(c)/2]
}

type item struct {
	snap   gate.Snapshot
	arrive float64
}

type DelayLine struct {
	Delay      float64 // 편도 지연(초). Down이 nil일 때만 쓴다
	Down       *Link   // 서버→클라이언트 링크. nil이면 고정 지연
	Up         *Link   // 클라이언트→서버(ack) 링크. Meter가 있을 때만 쓴다
	Meter      *Meter
	queue      []item
	last       gate.Snapshot
	lastArrive float64
}

// LastArrival: 가장 최근에 Push한 스냅샷이 클라이언트에 도착하는 시각.
func (d *DelayLine) LastArrival() float64 { return d.lastArrive }

func (d *DelayLine) Push(s gate.Snapshot) {
	arrive := s.Time + d.Delay
	if d.Down != nil {
		arrive = d.Down.Arrive(s.Time)
	}
	if d.Meter != nil && d.Up != nil {
		ack := d.Up.Arrive(arrive)
		d.Meter.add(ack, ((arrive-s.Time)+(ack-arrive))/2)
	}
	d.lastArrive = arrive
	d.queue = append(d.queue, item{s, arrive})
}

// View: now 시점에 클라이언트가 가진 가장 최신 스냅샷.
func (d *DelayLine) View(now float64) gate.Snapshot {
	for len(d.queue) > 0 && d.queue[0].arrive <= now+1e-9 {
		d.last = d.queue[0].snap
		d.queue = d.queue[1:]
	}
	return d.last
}
