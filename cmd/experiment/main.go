// 실험 1: 가시성 게이트가 월핵 이득을 얼마나 지우는가.
//
//	go run ./cmd/experiment -rounds 4000 -latency 20
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"

	"github.com/s26059-maker/anchor/internal/duel"
	"github.com/s26059-maker/anchor/internal/gate"
)

type row struct {
	label string
	gate  gate.Gate
}

type agg struct {
	win, lead float64
	n, to     int
	st        gate.Stats
}

func run(cfg duel.Config, rounds int, seed uint64) agg {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	dr := rand.New(rand.NewPCG(seed^0xD3C0FEED, seed^0x1234567890ABCDEF))
	var a agg
	for i := 0; i < rounds; i++ {
		res := duel.Round(cfg, r, dr, &a.st)
		if res.Timeout {
			a.to++
			continue
		}
		a.n++
		if res.HolderWon {
			a.win++
		}
		a.lead += res.InfoLead
	}
	if a.n > 0 {
		a.win /= float64(a.n)
		a.lead /= float64(a.n)
	}
	return a
}

func main() {
	rounds := flag.Int("rounds", 4000, "설정당 라운드 수")
	latMs := flag.Float64("latency", 20, "편도 지연(ms)")
	seed := flag.Uint64("seed", 7, "난수 시드")
	flag.Parse()
	lat := *latMs / 1000

	rows := []row{{label: "끔", gate: gate.Gate{}}}
	for _, ms := range []float64{0, 20, 50, 100, 200} {
		rows = append(rows, row{label: "켬", gate: gate.Gate{Enabled: true, Lookahead: ms / 1000, Samples: 8}})
	}

	fmt.Printf("코너 피크 1대1, 라운드 %d개씩, 편도 지연 %.0fms\n\n", *rounds, *latMs)
	fmt.Println("| 게이트 | 선공개(ms) | 정직한 홀더 승률 | 월핵 홀더 승률 | 월핵 이득 | 공정성 왜곡 | 숨은 적 누출률 | 정보 선행(ms) |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")
	// 공정성 기준: 게이트 없이 정직한 두 사람이 붙었을 때의 홀더 승률
	base := run(duel.Config{Cheat: false, Gate: gate.Gate{}, Latency: lat}, *rounds, *seed)
	for _, rw := range rows {
		honest := run(duel.Config{Cheat: false, Gate: rw.gate, Latency: lat}, *rounds, *seed)
		cheat := run(duel.Config{Cheat: true, Trust: 1, Gate: rw.gate, Latency: lat}, *rounds, *seed)
		look := "-"
		if rw.gate.Enabled {
			look = fmt.Sprintf("%.0f", rw.gate.Lookahead*1000)
		}
		// 공정성 왜곡: 선공개가 지연보다 짧으면 적이 늦게 튀어나와(pop-in) 정직한 승부가 기운다
		fmt.Printf("| %s | %s | %.1f%% | %.1f%% | %+.1f%%p | %+.1f%%p | %.2f%% | %.0f |\n",
			rw.label, look, 100*honest.win, 100*cheat.win, 100*(cheat.win-honest.win),
			100*(honest.win-base.win), 100*cheat.st.LeakRatio(), 1000*cheat.lead)
	}
}
