// Package evidence: 계정별 증거 누적. 대안 혼합 e-value.
//
// 정직한 사람이 미끼 쪽으로 우연히 에임을 옮길 확률이 P0 이하라면,
// 몇 번을 들여다봐도 P(언젠가 E ≥ 1/α) ≤ α 가 보장된다 (Ville 부등식).
// 고정 대안 하나만 쓰면 반응을 줄인 치트를 못 잡기 때문에, 여러 대안을 섞는다.
package evidence

import "math"

type Mixture struct {
	P0   float64
	a, b []float64 // 대안별 적중/비적중 시 로그 가능도비
	logs []float64 // 대안별 누적 로그 가능도비
	N    int       // 관측 수
}

func NewMixture(p0 float64, m int) *Mixture {
	x := &Mixture{P0: p0}
	lo := math.Log(p0/(1-p0)) + 0.01
	hi := math.Log(99.0)
	for i := 0; i < m; i++ {
		z := lo + (hi-lo)*float64(i)/float64(m-1)
		p := 1 / (1 + math.Exp(-z))
		x.a = append(x.a, math.Log(p/p0))
		x.b = append(x.b, math.Log((1-p)/(1-p0)))
		x.logs = append(x.logs, 0)
	}
	return x
}

func (x *Mixture) Observe(hit bool) {
	for j := range x.logs {
		if hit {
			x.logs[j] += x.a[j]
		} else {
			x.logs[j] += x.b[j]
		}
	}
	x.N++
}

// LogE: 현재 e-value의 자연로그.
func (x *Mixture) LogE() float64 {
	mx := math.Inf(-1)
	for _, v := range x.logs {
		mx = math.Max(mx, v)
	}
	s := 0.0
	for _, v := range x.logs {
		s += math.Exp(v - mx)
	}
	return mx + math.Log(s) - math.Log(float64(len(x.logs)))
}

func (x *Mixture) Over(alpha float64) bool { return x.LogE() >= math.Log(1/alpha) }
