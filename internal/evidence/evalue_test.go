package evidence

import (
	"math"
	"math/rand/v2"
	"testing"
)

// Ville 부등식: 정직한 계정(진짜 반응률 = p0)이라면, 아무리 오래 들여다봐도
// P(언젠가 E >= x) <= 1/x. 몬테카를로로 이 상한이 크게 깨지지 않는지 확인한다
// (증명을 검증하는 게 아니라 a/b 계산에 부호·인덱스 실수가 없는지 잡아내는 회귀 테스트).
func TestMixtureVilleBound(t *testing.T) {
	const p0 = 0.1
	const obsPerAccount = 2000
	const accounts = 8000

	for _, x := range []float64{10, 100} {
		alpha := 1 / x
		r := rand.New(rand.NewPCG(uint64(x)*7+1, 99))
		everOver := 0
		for a := 0; a < accounts; a++ {
			mx := NewMixture(p0, 40)
			for k := 0; k < obsPerAccount; k++ {
				hit := r.Float64() < p0
				mx.Observe(hit)
				if mx.Over(alpha) {
					everOver++
					break
				}
			}
		}
		rate := float64(everOver) / float64(accounts)
		// 몬테카를로 표본 오차를 감안해 상한의 1.5배까지만 허용한다.
		limit := 1.5 * alpha
		if rate > limit {
			t.Fatalf("x=%.0f: P(언젠가 E>=x) 실측 %.4f, 허용 상한 %.4f (이론상 <= %.4f)", x, rate, limit, alpha)
		}
	}
}

func TestMixtureCheatDetected(t *testing.T) {
	const p0 = 0.1
	r := rand.New(rand.NewPCG(1, 2))
	mx := NewMixture(p0, 40)
	// 진짜 반응률이 p0보다 훨씬 높은 "치트"를 흉내낸다 — e-value가 결국 커져야 한다.
	for k := 0; k < 2000; k++ {
		mx.Observe(r.Float64() < 0.8)
	}
	if !mx.Over(1e-9) {
		t.Fatalf("반응률 0.8인 계정 2000회 관측 후에도 log10E = %.2f로 α=1e-9를 못 넘음", mx.LogE()/math.Ln10)
	}
}
