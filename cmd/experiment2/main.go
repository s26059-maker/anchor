// 실험 2: 선공개 구간 미끼가 남은 월핵 이득을 지우고, 월핵을 확정하는가.
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
}

var strategies = []strategy{
	{"정직", false, 0},
	{"월핵 (신호 100% 믿음)", true, 1},
	{"월핵 (50%만 믿음)", true, 0.5},
	{"월핵 (20%만 믿음)", true, 0.2},
	{"월핵 (5%만 믿음)", true, 0.05},
}

func newRng(seed, k uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, k*0x9e3779b97f4a7c15+1)) }

// winRate: 라운드 승률과 미끼 반응률
func winRate(cfg duel.Config, rounds int, seed uint64) (win, resp float64, nEv int) {
	r := newRng(seed, 1)
	n, hit := 0, 0
	for i := 0; i < rounds; i++ {
		res := duel.Round(cfg, r, nil)
		if res.DecoyR >= 0 {
			nEv++
			hit += res.DecoyR
		}
		if res.Timeout {
			continue
		}
		n++
		if res.HolderWon {
			win++
		}
	}
	if n > 0 {
		win /= float64(n)
	}
	if nEv > 0 {
		resp = float64(hit) / float64(nEv)
	}
	return
}

func pct(xs []int, q float64) int {
	if len(xs) == 0 {
		return -1
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	return s[int(q*float64(len(s)-1))]
}

func main() {
	rounds := flag.Int("rounds", 4000, "승률 측정 라운드 수")
	latMs := flag.Float64("latency", 20, "편도 지연(ms)")
	lookMs := flag.Float64("lookahead", 20, "선공개 구간(ms)")
	accounts := flag.Int("accounts", 200, "전략당 계정 수")
	maxRounds := flag.Int("maxrounds", 500, "계정당 최대 라운드")
	alpha := flag.Float64("alpha", 1e-9, "판정 수준 α")
	p0Flag := flag.Float64("p0", 0, "정직한 반응률 상한 (0이면 측정값의 2배)")
	seed := flag.Uint64("seed", 11, "난수 시드")
	flag.Parse()

	g := gate.Gate{Enabled: true, Lookahead: *lookMs / 1000, Samples: 8}
	base := duel.Config{Gate: g, Latency: *latMs / 1000, Holds: true}

	// ---- A. 승률: 미끼가 이득을 지우는가
	fmt.Printf("## A. 승률 (게이트 켬, 선공개 %.0fms, 지연 %.0fms, 라운드 %d개)\n\n", *lookMs, *latMs, *rounds)
	fmt.Println("| 홀더 전략 | 미끼 끔 승률 | 미끼 켬 승률 | 이득(미끼 켬, 정직 대비) | 미끼 반응률 |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	var honestOn, honestOff, p0hat float64
	for i, s := range strategies {
		c := base
		c.Cheat, c.Trust = s.cheat, s.trust
		off, _, _ := winRate(c, *rounds, *seed)
		c.Decoys = true
		on, resp, _ := winRate(c, *rounds, *seed)
		if i == 0 {
			honestOn, honestOff, p0hat = on, off, resp
		}
		fmt.Printf("| %s | %.1f%% | %.1f%% | %+.1f%%p | %.1f%% |\n", s.name, 100*off, 100*on, 100*(on-honestOn), 100*resp)
	}
	fmt.Printf("\n공정성: 정직한 홀더 승률 미끼 끔 %.1f%% → 켬 %.1f%% (%+.1f%%p)\n\n", 100*honestOff, 100*honestOn, 100*(honestOn-honestOff))

	// ---- B. 판정: 계정별 e-value
	p0 := *p0Flag
	if p0 <= 0 {
		p0 = math.Max(0.05, 2*p0hat)
	}
	fmt.Printf("## B. 판정 (p₀ = %.3f, 측정된 정직 반응률 %.3f의 2배, α = %.0e, 계정 %d개 × 최대 %d라운드)\n\n", p0, p0hat, *alpha, *accounts, *maxRounds)
	fmt.Println("| 홀더 전략 | 밴 비율 | 밴까지 라운드(중앙값) | 95% 계정 밴까지 | 최대 log₁₀E(밴 안 된 계정) |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	c := base
	c.Decoys = true
	for si, s := range strategies {
		c.Cheat, c.Trust = s.cheat, s.trust
		var banAt []int
		maxLog := math.Inf(-1)
		for a := 0; a < *accounts; a++ {
			r := newRng(*seed, uint64(1000*(si+1)+a))
			ev := evidence.NewMixture(p0, 40)
			banned := false
			for k := 1; k <= *maxRounds; k++ {
				res := duel.Round(c, r, nil)
				if res.DecoyR < 0 {
					continue
				}
				ev.Observe(res.DecoyR == 1)
				if ev.Over(*alpha) {
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
		if len(banAt) >= int(0.95*float64(*accounts)) {
			p95 = fmt.Sprint(pct(banAt, 0.95))
		}
		fmt.Printf("| %s | %.1f%% | %s | %s | %s |\n", s.name, 100*float64(len(banAt))/float64(*accounts), med, p95, ml)
	}
}
