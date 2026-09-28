// 실험 2: 선공개 구간 미끼가 남은 월핵 이득을 지우고, 월핵을 확정하는가.
// 2.5단계: 개수로 미끼를 거르는 SmartWallhack 레드팀과 그 방어 후보들.
//
//	go run ./cmd/experiment2
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"

	"github.com/s26059-maker/anchor/internal/duel"
	"github.com/s26059-maker/anchor/internal/evidence"
	"github.com/s26059-maker/anchor/internal/gate"
)

type strategy struct {
	name  string
	cheat bool
	trust float64
	smart bool // true면 SmartWallhack (개수로 미끼를 거르고, 쌍 신호는 타이밍에만 쓴다)
}

var strategies = []strategy{
	{"정직", false, 0, false},
	{"월핵 (신호 100% 믿음)", true, 1, false},
	{"월핵 (50%만 믿음)", true, 0.5, false},
	{"월핵 (20%만 믿음)", true, 0.2, false},
	{"월핵 (5%만 믿음)", true, 0.05, false},
	{"SmartWallhack (개수로 미끼 거름)", true, 0, true},
}

func apply(c duel.Config, s strategy) duel.Config {
	c.Cheat, c.Trust, c.Smart = s.cheat, s.trust, s.smart
	return c
}

// 시나리오 rng와 미끼 rng를 별도 스트림으로 준다 (과제 1). key가 같으면 둘 다 같은 스트림을 재현한다.
func newRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, key*0x9e3779b97f4a7c15+1))
}
func newDecoyRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed^0xD3C0FEED, key*0x9e3779b97f4a7c15+1))
}

type stats struct {
	win   float64
	n     int
	resp  float64 // 무작위 미끼 반응률 (DecoyR)
	nEv   int
	fast  float64 // "비정상으로 빠른" 반응 비율 (FastR)
	nFast int
}

func runRounds(cfg duel.Config, rounds int, seed uint64) stats {
	r := newRng(seed, 1)
	dr := newDecoyRng(seed, 1)
	var s stats
	var hit, fhit int
	for i := 0; i < rounds; i++ {
		res := duel.Round(cfg, r, dr, nil)
		if res.DecoyR >= 0 {
			s.nEv++
			hit += res.DecoyR
		}
		if res.FastR >= 0 {
			s.nFast++
			fhit += res.FastR
		}
		if res.Timeout {
			continue
		}
		s.n++
		if res.HolderWon {
			s.win++
		}
	}
	if s.n > 0 {
		s.win /= float64(s.n)
	}
	if s.nEv > 0 {
		s.resp = float64(hit) / float64(s.nEv)
	}
	if s.nFast > 0 {
		s.fast = float64(fhit) / float64(s.nFast)
	}
	return s
}

// se95: 이항 비율 phat(시행 n회)의 95% 신뢰구간 반폭.
func se95(p float64, n int) float64 {
	if n <= 0 {
		return 0
	}
	return 1.96 * math.Sqrt(p*(1-p)/float64(n))
}

func pct(xs []int, q float64) int {
	if len(xs) == 0 {
		return -1
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	return s[int(q*float64(len(s)-1))]
}

// banTable: 계정별 e-value 판정 시뮬레이션. extract가 라운드에서 증거값(-1/0/1)을 뽑는다.
func banTable(c duel.Config, p0 float64, accounts, maxRounds int, alpha float64, seed, keyBase uint64, extract func(duel.Result) int) {
	fmt.Println("| 홀더 전략 | 밴 비율 | 밴까지 라운드(중앙값) | 95% 계정 밴까지 | 최대 log₁₀E(밴 안 된 계정) |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	for si, s := range strategies {
		cc := apply(c, s)
		var banAt []int
		maxLog := math.Inf(-1)
		for a := 0; a < accounts; a++ {
			key := keyBase + uint64(1000*(si+1)+a)
			r := newRng(seed, key)
			dr := newDecoyRng(seed, key)
			ev := evidence.NewMixture(p0, 40)
			banned := false
			for k := 1; k <= maxRounds; k++ {
				res := duel.Round(cc, r, dr, nil)
				rv := extract(res)
				if rv < 0 {
					continue
				}
				ev.Observe(rv == 1)
				if ev.Over(alpha) {
					banAt = append(banAt, k)
					banned = true
					break
				}
			}
			if !banned {
				maxLog = math.Max(maxLog, ev.LogE()/math.Ln10)
			}
		}
		ml := "-"
		if !math.IsInf(maxLog, -1) {
			ml = fmt.Sprintf("%.1f", maxLog)
		}
		med, p95 := "-", "-"
		if len(banAt) > 0 {
			med = fmt.Sprint(pct(banAt, 0.5))
		}
		if len(banAt) >= int(0.95*float64(accounts)) {
			p95 = fmt.Sprint(pct(banAt, 0.95))
		}
		fmt.Printf("| %s | %.1f%% | %s | %s | %s |\n", s.name, 100*float64(len(banAt))/float64(accounts), med, p95, ml)
	}
}

func main() {
	rounds := flag.Int("rounds", 20000, "승률 측정 라운드 수")
	latMs := flag.Float64("latency", 20, "편도 지연(ms)")
	lookMs := flag.Float64("lookahead", 20, "선공개 구간(ms)")
	accounts := flag.Int("accounts", 200, "전략당 계정 수")
	maxRounds := flag.Int("maxrounds", 500, "계정당 최대 라운드")
	alpha := flag.Float64("alpha", 1e-9, "판정 수준 α")
	p0Flag := flag.Float64("p0", 0, "정직한 반응률 상한 (0이면 측정값의 2배)")
	pairFreq := flag.Float64("pairfreq", 0, "무작위 미끼를 거울 쌍으로 보낼 확률 (방어 a/b, A·B 표에 적용)")
	sweepRounds := flag.Int("sweeprounds", 8000, "C·D 표(방어 후보 비교)용 라운드 수")
	seed := flag.Uint64("seed", 11, "난수 시드")
	flag.Parse()

	g := gate.Gate{Enabled: true, Lookahead: *lookMs / 1000, Samples: 8}
	base := duel.Config{Gate: g, Latency: *latMs / 1000, Holds: true, DecoyPairFreq: *pairFreq}

	// ---- A. 승률: 미끼가 이득을 지우는가
	fmt.Printf("## A. 승률 (게이트 켬, 선공개 %.0fms, 지연 %.0fms, 쌍 빈도 %.0f%%, 라운드 %d개)\n\n", *lookMs, *latMs, 100*(*pairFreq), *rounds)
	fmt.Println("| 홀더 전략 | 미끼 끔 승률 | 미끼 켬 승률 | 이득(미끼 켬, 정직 대비) | 미끼 반응률 |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	var honestOn, honestOff, p0hat float64
	var honestOnN, honestOffN int
	for i, s := range strategies {
		c := apply(base, s)
		off := runRounds(c, *rounds, *seed)
		c.Decoys = true
		on := runRounds(c, *rounds, *seed)
		if i == 0 {
			honestOn, honestOff, p0hat = on.win, off.win, on.resp
			honestOnN, honestOffN = on.n, off.n
		}
		gain := on.win - honestOn
		gainSE := 1.96 * math.Sqrt(math.Pow(se95(on.win, on.n)/1.96, 2)+math.Pow(se95(honestOn, honestOnN)/1.96, 2))
		fmt.Printf("| %s | %.1f%% ± %.1f%%p | %.1f%% ± %.1f%%p | %+.1f%%p ± %.1f%%p | %.1f%% |\n",
			s.name, 100*off.win, 100*se95(off.win, off.n), 100*on.win, 100*se95(on.win, on.n),
			100*gain, 100*gainSE, 100*on.resp)
	}
	fmt.Printf("\n공정성: 정직한 홀더 승률 미끼 끔 %.1f%% ± %.1f%%p → 켬 %.1f%% ± %.1f%%p (%+.1f%%p)\n\n",
		100*honestOff, 100*se95(honestOff, honestOffN), 100*honestOn, 100*se95(honestOn, honestOnN), 100*(honestOn-honestOff))

	// ---- B. 판정: 계정별 e-value (무작위 미끼 반응, 기존 증거 규칙)
	p0 := *p0Flag
	if p0 <= 0 {
		p0 = math.Max(0.05, 2*p0hat)
	}
	fmt.Printf("## B. 판정 — 무작위 미끼 반응 증거 (p₀ = %.3f, 측정된 정직 반응률 %.3f의 2배, α = %.0e, 계정 %d개 × 최대 %d라운드)\n\n",
		p0, p0hat, *alpha, *accounts, *maxRounds)
	bc := base
	bc.Decoys = true
	banTable(bc, p0, *accounts, *maxRounds, *alpha, *seed, 100000, func(r duel.Result) int { return r.DecoyR })

	// ---- C. 방어 (a)/(b): 무작위 미끼를 거울 쌍으로 보내는 빈도별 SmartWallhack 이득
	fmt.Printf("\n## C. 방어 (a)/(b): 무작위 미끼 쌍 빈도별 SmartWallhack 이득 (선공개 %.0fms, 지연 %.0fms, 라운드 %d개)\n\n", *lookMs, *latMs, *sweepRounds)
	fmt.Println("| 쌍 빈도 | 정직 승률 | SmartWallhack 승률 | 이득(정직 대비) | 미끼 반응률 |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	smart := strategies[len(strategies)-1]
	for _, pf := range []float64{0, 0.25, 0.5, 0.75, 1.0} {
		c := base
		c.Decoys = true
		c.DecoyPairFreq = pf
		hc := apply(c, strategies[0])
		honest := runRounds(hc, *sweepRounds, *seed)
		sc := apply(c, smart)
		on := runRounds(sc, *sweepRounds, *seed)
		fmt.Printf("| %.0f%% | %.1f%% | %.1f%% | %+.1f%%p | %.1f%% |\n",
			100*pf, 100*honest.win, 100*on.win, 100*(on.win-honest.win), 100*on.resp)
	}

	// ---- D. 방어 (c): 선공개·지연별 SmartWallhack 잔여 이득 (타이밍 누출 상한)
	fmt.Printf("\n## D. 방어 (c): 선공개·지연별 SmartWallhack 잔여 이득 (쌍 빈도 100%%, 라운드 %d개)\n\n", *sweepRounds)
	fmt.Println("| 선공개(ms) | 지연(ms) | 선공개-지연(ms) | 정직 승률 | SmartWallhack 승률 | 이득 |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	combos := []struct{ look, lat float64 }{
		{20, 20}, {50, 20}, {100, 20}, {20, 50}, {50, 50}, {100, 100}, {200, 20},
	}
	for _, cb := range combos {
		cg := gate.Gate{Enabled: true, Lookahead: cb.look / 1000, Samples: 8}
		c := duel.Config{Gate: cg, Latency: cb.lat / 1000, Holds: true, Decoys: true, DecoyPairFreq: 1.0}
		hc := apply(c, strategies[0])
		honest := runRounds(hc, *sweepRounds, *seed)
		sc := apply(c, smart)
		on := runRounds(sc, *sweepRounds, *seed)
		fmt.Printf("| %.0f | %.0f | %.0f | %.1f%% | %.1f%% | %+.1f%%p |\n",
			cb.look, cb.lat, cb.look-cb.lat, 100*honest.win, 100*on.win, 100*(on.win-honest.win))
	}

	// ---- E. 방어 (d): 반응 지연 기반 새 증거 규칙 (쌍 빈도 100% 방어 채택 후에도 잡히는가)
	fmt.Printf("\n## E. 방어 (d): 반응 지연 기반 새 증거 규칙 (쌍 빈도 100%%, 선공개 %.0fms, 지연 %.0fms)\n\n", *lookMs, *latMs)
	ec := base
	ec.Decoys = true
	ec.DecoyPairFreq = 1.0
	ehc := apply(ec, strategies[0])
	ehonest := runRounds(ehc, *rounds, *seed)
	p0fast := *p0Flag
	if p0fast <= 0 {
		p0fast = math.Max(0.001, 2*ehonest.fast)
	}
	fmt.Printf("p₀(반응 지연 < %.0fms) = %.4f (측정된 정직 비율 %.4f의 2배, 표본 %d개), α = %.0e\n\n", 1000*duel.FastReactThr, p0fast, ehonest.fast, ehonest.nFast, *alpha)
	banTable(ec, p0fast, *accounts, *maxRounds, *alpha, *seed, 200000, func(r duel.Result) int { return r.FastR })
}
