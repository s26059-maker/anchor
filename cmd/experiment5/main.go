// 실험 5: 2.8단계 — 남은 한계의 크기를 정확히 잰다.
//
//	A. 지연 추정을 평균+m 대신 분위수+m으로 (그리고 스파이크 허용) — 공정성 손실 ≤ 1%p 중 누출 최소
//	B. 핑 환산 곡선: 정직 대 정직에서 피커의 편도 지연만 Δ 낮췄을 때 피커 승률 변화
//	C. 남은 피커 치트 이득(신뢰100%, 검증형)을 핑 몇 ms 이득으로 환산
//
//	go run ./cmd/experiment5
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sync"

	"github.com/s26059-maker/anchor/internal/duel"
	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/netsim"
)

func newRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, key*0x9e3779b97f4a7c15+1))
}
func newDecoyRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed^0xD3C0FEED, key*0x9e3779b97f4a7c15+1))
}

type outcome struct {
	win  []int8 // 1 홀더 승, 0 홀더 패, -1 시간 초과
	pop  []float32
	leak []float32
}

type spec struct {
	cfg    duel.Config
	rounds int
	seed   uint64
	key    uint64
}

func exec(s spec) *outcome {
	r, dr := newRng(s.seed, s.key), newDecoyRng(s.seed, s.key)
	o := &outcome{}
	for i := 0; i < s.rounds; i++ {
		res := duel.Round(s.cfg, r, dr, nil)
		w := int8(-1)
		if !res.Timeout {
			w = 0
			if res.HolderWon {
				w = 1
			}
		}
		o.win = append(o.win, w)
		o.pop = append(o.pop, float32(res.PopIn))
		o.leak = append(o.leak, float32(res.Leak))
	}
	return o
}

func execAll(specs []spec) []*outcome {
	out := make([]*outcome, len(specs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i := range specs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			out[i] = exec(specs[i])
			<-sem
		}(i)
	}
	wg.Wait()
	return out
}

// diff: a − b 홀더 승률 차이와 95% CI 반폭(라운드별 짝지은 차이).
func diff(a, b *outcome) (float64, float64) {
	var d []float64
	for i := range a.win {
		if a.win[i] >= 0 && b.win[i] >= 0 {
			d = append(d, float64(a.win[i]-b.win[i]))
		}
	}
	if len(d) < 2 {
		return 0, 0
	}
	m := 0.0
	for _, x := range d {
		m += x
	}
	m /= float64(len(d))
	v := 0.0
	for _, x := range d {
		v += (x - m) * (x - m)
	}
	return m, 1.96 * math.Sqrt(v/float64(len(d)-1)/float64(len(d)))
}

func meanNonNeg(xs []float32) float64 {
	s, n := 0.0, 0
	for _, x := range xs {
		if x >= 0 {
			s += float64(x)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return s / float64(n)
}

func ppci(m, h float64) string { return fmt.Sprintf("%+.1f ± %.1f", 100*m, 100*h) }

var (
	cheatTrust  = duel.Config{Cheat: true, Trust: 1, CheatRole: duel.RolePeeker}
	cheatVerify = duel.Config{Cheat: true, Trust: 1, CheatRole: duel.RolePeeker, GeoWindow: 0.15, VerifyTicks: 3}
)

func withCheat(c, kind duel.Config) duel.Config {
	c.Cheat, c.Trust, c.CheatRole, c.GeoWindow, c.VerifyTicks = kind.Cheat, kind.Trust, kind.CheatRole, kind.GeoWindow, kind.VerifyTicks
	return c
}

func model(mean, sd, sp, ss float64) *netsim.Model {
	return &netsim.Model{Mean: mean / 1000, Jitter: sd / 1000, SpikeProb: sp, SpikeSize: ss / 1000}
}

// policy: 선공개 정책 하나.
type policy struct {
	name   string
	fixed  float64 // ms (>0이면 고정)
	q      float64
	window int
	margin float64 // ms
	gap    float64 // ms, 스파이크 허용
}

func (p policy) apply(c duel.Config) duel.Config {
	if p.fixed > 0 {
		c.Gate.Lookahead = p.fixed / 1000
		return c
	}
	c.Adaptive, c.Margin, c.MeterQuantile, c.MeterWindow, c.SpikeGap = true, p.margin/1000, p.q, p.window, p.gap/1000
	return c
}

func base(net *netsim.Model) duel.Config {
	return duel.Config{
		Gate:    gate.Gate{Enabled: true, Mode: gate.ViewerOnly, Samples: 8},
		Latency: net.Mean, Net: net, Holds: true,
	}
}

func offCfg(net *netsim.Model) duel.Config {
	return duel.Config{Latency: net.Mean, Net: net, Holds: true}
}

var jitters = []struct {
	name       string
	sd, sp, ss float64
}{
	{"σ=5ms", 5, 0, 0},
	{"σ=10ms", 10, 0, 0},
	{"σ=5ms+스파이크(2%,+60ms)", 5, 0.02, 60},
}

func main() {
	rounds := flag.Int("rounds", 20000, "설정당 라운드 수")
	seed := flag.Uint64("seed", 13, "난수 시드")
	maxLoss := flag.Float64("maxloss", 1.0, "공정성 손실 허용치(%p)")
	gap := flag.Float64("gap", 25, "스파이크 허용의 판정 간격(ms): 중앙값보다 이만큼 넘게 늦으면 스파이크")
	flag.Parse()
	R := *rounds

	// ---------- 후보 정책
	var cands []policy
	for _, m := range []float64{0, 5, 10, 20, 30, 45, 60} {
		cands = append(cands, policy{name: fmt.Sprintf("평균(N=16)+%.0f", m), window: 16, margin: m})
	}
	for _, q := range []float64{0.90, 0.95, 0.99} {
		for _, m := range []float64{0, 5, 10, 20} {
			cands = append(cands, policy{name: fmt.Sprintf("p%.0f(N=64)+%.0f", 100*q, m), q: q, window: 64, margin: m})
		}
	}
	all := append([]policy(nil), cands...)
	for _, p := range cands {
		p.name += " +스파이크허용"
		p.gap = *gap
		all = append(all, p)
	}

	// ================= A. 추정기 탐색
	fmt.Printf("## A. 지연 추정기 × 스파이크 허용 (ViewerOnly, 평균 지연 20ms, 라운드 %d개, 스파이크 판정 간격 %.0fms)\n", R, *gap)
	type res struct {
		p          policy
		loss, ci   float64
		pop, leak  float64
		pass       bool
		specialTag string
	}
	best := map[string]map[string]*res{} // 지터 → 부류 → 최선
	for ji, j := range jitters {
		net := model(20, j.sd, j.sp, j.ss)
		key := uint64(500 + ji)
		specs := []spec{{offCfg(net), R, *seed, key}}
		for _, p := range all {
			specs = append(specs, spec{p.apply(base(net)), R, *seed, key})
		}
		outs := execAll(specs)
		fmt.Printf("\n### 지터: %s\n\n| 정책 | 공정성 손실(pp) | 팝인 평균(ms) | 누출 평균(ms) | 손실 ≤ %.1fpp |\n| --- | --- | --- | --- | --- |\n", j.name, *maxLoss)
		best[j.name] = map[string]*res{}
		for i, p := range all {
			o := outs[i+1]
			m, h := diff(o, outs[0])
			r := &res{p: p, loss: m, ci: h, pop: 1000 * meanNonNeg(o.pop), leak: 1000 * meanNonNeg(o.leak)}
			r.pass = 100*m <= *maxLoss
			mark := ""
			if r.pass {
				mark = "✓"
			}
			fmt.Printf("| %s | %s | %.1f | %.1f | %s |\n", p.name, ppci(m, h), r.pop, r.leak, mark)
			if !r.pass {
				continue
			}
			class := "평균"
			if p.q > 0 {
				class = "분위수"
			}
			if p.gap > 0 {
				class += "+스파이크허용"
			}
			for _, k := range []string{class, "전체"} {
				if b := best[j.name][k]; b == nil || r.leak < b.leak {
					best[j.name][k] = r
				}
			}
		}
	}
	fmt.Printf("\n### A 요약 — 손실 ≤ %.1fpp를 지키는 정책 중 누출 최소\n\n| 지터 | 부류 | 정책 | 손실(pp) | 누출(ms) |\n| --- | --- | --- | --- | --- |\n", *maxLoss)
	classes := []string{"평균", "분위수", "평균+스파이크허용", "분위수+스파이크허용", "전체"}
	for _, j := range jitters {
		for _, c := range classes {
			if b := best[j.name][c]; b != nil {
				fmt.Printf("| %s | %s | %s | %s | %.1f |\n", j.name, c, b.p.name, ppci(b.loss, b.ci), b.leak)
			} else {
				fmt.Printf("| %s | %s | (조건을 만족하는 정책 없음) | | |\n", j.name, c)
			}
		}
	}

	// ================= B. 핑 환산 곡선
	deltas := []float64{0, 5, 10, 15, 20, 30, 40}
	const holderMs, peekerMs = 20.0, 50.0
	fmt.Printf("\n## B. 핑 환산 곡선 — 정직 대 정직, 홀더 편도 %.0fms, 피커 편도 %.0fms에서 Δ만 낮춤 (사격 등록 지연 켬, 라운드 %d개)\n\n", holderMs, peekerMs, R)
	type curve struct {
		name string
		y    []float64 // Δ별 피커 승률 변화(Δ=0 대비)
		ci   []float64
	}
	var curves []curve
	curveKey := uint64(600)
	for _, cv := range []struct {
		name  string
		gated bool
		sd    float64
	}{{"게이트 끔, 지터 없음", false, 0}, {"게이트 끔, σ=10ms", false, 10}, {"ViewerOnly 적응형 평균+20, 지터 없음", true, 0}} {
		var specs []spec
		for _, d := range deltas {
			net := model(holderMs, cv.sd, 0, 0)
			c := offCfg(net)
			if cv.gated {
				c = policy{window: 16, margin: 20}.apply(base(net))
			}
			pn := model(peekerMs-d, cv.sd, 0, 0)
			c.PeekerNet, c.ShotLatency = pn, true
			specs = append(specs, spec{c, R, *seed, curveKey})
		}
		curveKey++
		outs := execAll(specs)
		c := curve{name: cv.name}
		for i := range deltas {
			m, h := diff(outs[i], outs[0]) // 홀더 승률 변화 → 피커는 부호 반대
			c.y, c.ci = append(c.y, -m), append(c.ci, h)
		}
		curves = append(curves, c)
	}
	fmt.Print("| Δ(ms) |")
	for _, c := range curves {
		fmt.Printf(" %s: 피커 승률 변화(pp) |", c.name)
	}
	fmt.Print("\n| --- |")
	for range curves {
		fmt.Print(" --- |")
	}
	fmt.Println()
	for i, d := range deltas {
		fmt.Printf("| %.0f |", d)
		for _, c := range curves {
			fmt.Printf(" %s |", ppci(c.y[i], c.ci[i]))
		}
		fmt.Println()
	}
	toMs := func(c curve, g float64) string { // 이득 g(0~1) → Δ(ms). 선형 보간
		if g <= 0 {
			return "0"
		}
		for i := 1; i < len(deltas); i++ {
			if g <= c.y[i] {
				if c.y[i] == c.y[i-1] {
					return fmt.Sprintf("%.0f", deltas[i])
				}
				return fmt.Sprintf("%.0f", deltas[i-1]+(deltas[i]-deltas[i-1])*(g-c.y[i-1])/(c.y[i]-c.y[i-1]))
			}
		}
		return fmt.Sprintf(">%.0f", deltas[len(deltas)-1])
	}
	ref := curves[0]

	// ================= C. 남은 치트 이득의 핑 환산
	fmt.Printf("\n## C. 남은 피커 치트 이득의 핑 환산 (홀더 편도 %.0fms, 피커 편도 %.0fms, 사격 등록 지연 켬, 환산 곡선: \"%s\", 라운드 %d개)\n\n", holderMs, peekerMs, ref.name, R)
	fmt.Println("이득은 같은 정책의 정직 피커 대비 피커 치트 승률 증가분. 환산 ms는 95% CI 양끝의 환산값을 괄호로 준다.")
	fmt.Println()
	fmt.Println("| 지터 | 정책 | 누출(ms) | 공정성 손실(pp) | 신뢰100% 이득(pp) | ≈ 핑 이득(ms) | 검증형 이득(pp) | ≈ 핑 이득(ms) |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")
	for ji, j := range jitters[1:] { // σ=10, 스파이크
		net := model(holderMs, j.sd, j.sp, j.ss)
		pnet := model(peekerMs, j.sd, j.sp, j.ss)
		key := uint64(700 + ji)
		mk := func(p policy) duel.Config {
			c := p.apply(base(net))
			c.PeekerNet, c.ShotLatency = pnet, true
			return c
		}
		offc := offCfg(net)
		offc.PeekerNet, offc.ShotLatency = pnet, true
		pols := []policy{
			{name: "고정 20ms", fixed: 20},
			{name: "고정 50ms", fixed: 50},
		}
		m27 := map[string]float64{"σ=10ms": 10, "σ=5ms+스파이크(2%,+60ms)": 45}[j.name]
		pols = append(pols, policy{name: fmt.Sprintf("2.7 평균(N=16)+%.0f", m27), window: 16, margin: m27})
		for _, c := range []string{"평균", "분위수", "평균+스파이크허용", "분위수+스파이크허용"} {
			if b := best[j.name][c]; b != nil {
				p := b.p
				p.name = "2.8 " + p.name
				pols = append(pols, p)
			}
		}
		specs := []spec{{offc, R, *seed, key}}
		for _, p := range pols {
			c := mk(p)
			specs = append(specs, spec{c, R, *seed, key}, spec{withCheat(c, cheatTrust), R, *seed, key}, spec{withCheat(c, cheatVerify), R, *seed, key})
		}
		outs := execAll(specs)
		for i, p := range pols {
			h, t, v := outs[1+3*i], outs[2+3*i], outs[3+3*i]
			lm, lh := diff(h, outs[0])
			gt, gth := diff(t, h)
			gv, gvh := diff(v, h)
			gt, gv = -gt, -gv
			conv := func(g, ci float64) string {
				return fmt.Sprintf("%s (%s ~ %s)", toMs(ref, g), toMs(ref, g-ci), toMs(ref, g+ci))
			}
			fmt.Printf("| %s | %s | %.1f | %s | %s | %s | %s | %s |\n", j.name, p.name, 1000*meanNonNeg(h.leak), ppci(lm, lh),
				ppci(gt, gth), conv(gt, gth), ppci(gv, gvh), conv(gv, gvh))
		}
	}
}
