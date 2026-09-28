// 실험 3: 2.6단계 — viewer 기준 선공개(gate.ViewerOnly)로 SmartWallhack의 타이밍 누출을
// 닫을 수 있는가. 역할 반전(피커 치트), 렌더링 가정(보간/외삽) 민감도, SmartWallhack의
// "사람 한계"(최소 예고 시간)까지 함께 측정한다.
//
//	go run ./cmd/experiment3
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/s26059-maker/anchor/internal/bot"
	"github.com/s26059-maker/anchor/internal/duel"
	"github.com/s26059-maker/anchor/internal/gate"
)

func newRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, key*0x9e3779b97f4a7c15+1))
}
func newDecoyRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed^0xD3C0FEED, key*0x9e3779b97f4a7c15+1))
}

type stats struct {
	win float64
	n   int
}

func run(cfg duel.Config, rounds int, seed, key uint64) stats {
	r := newRng(seed, key)
	dr := newDecoyRng(seed, key)
	var s stats
	var wins int
	for i := 0; i < rounds; i++ {
		res := duel.Round(cfg, r, dr, nil)
		if res.Timeout {
			continue
		}
		s.n++
		if res.HolderWon {
			wins++
		}
	}
	if s.n > 0 {
		s.win = float64(wins) / float64(s.n)
	}
	return s
}

func se95(p float64, n int) float64 {
	if n <= 0 {
		return 0
	}
	return 1.96 * math.Sqrt(p*(1-p)/float64(n))
}

// diffSE: 독립 이항 비율 두 개(p1,n1)와 (p2,n2)의 차이에 대한 95% CI 반폭.
func diffSE(p1 float64, n1 int, p2 float64, n2 int) float64 {
	a, b := n1, n2
	if a < 1 {
		a = 1
	}
	if b < 1 {
		b = 1
	}
	return 1.96 * math.Sqrt(p1*(1-p1)/float64(a)+p2*(1-p2)/float64(b))
}

type modeDef struct {
	mode gate.Mode
	name string
}

var modes = []modeDef{{gate.Both, "Both"}, {gate.ViewerOnly, "ViewerOnly"}}

var looks = []float64{0, 20, 50}

var roles = []struct {
	role duel.Role
	name string
}{{duel.RoleHolder, "홀더"}, {duel.RolePeeker, "피커"}}

// gateCfg: 미끼(2.5단계)를 기본으로 깐 채 게이트 모드를 바꾼다. 미끼가 없으면 SmartWallhack은
// 벽 뒤 신호를 항상 "혼자"로 보고 매번 무시해버려서(2.5단계 설계), 홀더 역할 테스트가 공허해진다.
//
// DecoyPairFreq는 0(무작위 미끼를 안 쌍으로 보냄)이다 — 2.5단계에서는 1.0을 권장했지만, 이번
// 실험 도중 ViewerOnly와 결합하면 그 권장이 오히려 새 구멍을 연다는 걸 발견했다(아래 D 표).
// ViewerOnly에서는 진짜 신호의 쌍(거울 미끼)이 아예 안 뜨기 때문에, 무작위 미끼를 쌍으로 보내면
// SmartWallhack이 진짜 노출과 무관한 그 쌍만으로 타이머를 미리 돌릴 근거를 얻는다. Both 모드에서는
// 쌍 빈도가 SmartWallhack 이득에 영향이 없었으므로(2.5단계 C 표), 0으로 둬도 손해가 없다.
func gateCfg(mode gate.Mode, lookMs, latency float64) duel.Config {
	return duel.Config{
		// Enabled는 항상 true — 선공개 0ms는 "게이트 꺼짐"이 아니라 "게이트 켬, 선공개 0"(엄격한
		// LOS 게이팅, 실험 1의 관례와 동일)이다.
		Gate:    gate.Gate{Enabled: true, Mode: mode, Lookahead: lookMs / 1000, Samples: 8},
		Latency: latency,
		Holds:   true,
		Decoys:  true,
	}
}

func withCheat(base duel.Config, role duel.Role, trust float64, smart bool, minLead float64) duel.Config {
	c := base
	c.CheatRole = role
	c.Cheat = true
	c.Trust = trust
	c.Smart = smart
	c.MinLead = minLead
	return c
}

func main() {
	rounds := flag.Int("rounds", 20000, "표 A/B 행마다 라운드 수")
	renderRounds := flag.Int("renderrounds", 8000, "표 C(렌더링 민감도)용 라운드 수")
	latMs := flag.Float64("latency", 20, "편도 지연(ms)")
	humanLeadMs := flag.Float64("humanlead", 100, "SmartWallhack 사람 한계: 최소 예고 시간(ms)")
	seed := flag.Uint64("seed", 13, "난수 시드")
	flag.Parse()
	lat := *latMs / 1000
	humanLead := *humanLeadMs / 1000

	offBase := duel.Config{Gate: gate.Gate{}, Latency: lat, Holds: true}
	off := run(offBase, *rounds, *seed, 0)

	// ---- A. 게이트 모드 × 선공개 × 치트 역할: 공정성과 전략별 이득
	//
	// stats.win은 항상 "홀더 승률"이다. 홀더가 치트면 이득 = 치트 홀더승률 − 정직 홀더승률(그대로).
	// 피커가 치트면 치터(피커) 자신의 승률은 (1 − 홀더승률)이므로 이득 = 정직 홀더승률 − 치트 홀더승률
	// (부호를 뒤집는다) — 두 경우 모두 "이득" 열은 항상 치트를 쓰는 쪽 자신의 승률 증가분이다.
	fmt.Printf("## A. 게이트 모드 × 선공개 × 치트 역할 (지연 %.0fms, 라운드 %d개, 짝지은 rng)\n\n", *latMs, *rounds)
	fmt.Printf("게이트 끔 기준(공정성 비교 원점): 정직 대 정직 승률 %.1f%% ± %.1f%%p\n\n", 100*off.win, 100*se95(off.win, off.n))
	fmt.Println("이득 열은 항상 \"치트를 쓰는 쪽 자신의\" 승률 증가분이다(피커가 치트면 부호를 뒤집어 계산).")
	fmt.Println("| 모드 | 선공개(ms) | 치트 역할 | 정직 대 정직 승률 | 공정성(게이트 끔 대비) | 신뢰100% 이득 | 신뢰50% 이득 | 신뢰20% 이득 | SmartWallhack(사람,100ms) 이득 |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for mi, md := range modes {
		for li, look := range looks {
			key := uint64(mi*100 + li)
			base := gateCfg(md.mode, look, lat)
			h := run(base, *rounds, *seed, key)
			fairness := h.win - off.win
			fairSE := diffSE(h.win, h.n, off.win, off.n)
			for _, rl := range roles {
				sign := 1.0
				if rl.role == duel.RolePeeker {
					sign = -1.0
				}
				row := fmt.Sprintf("| %s | %.0f | %s | %.1f%% ± %.1f%%p | %+.1f%%p ± %.1f%%p",
					md.name, look, rl.name, 100*h.win, 100*se95(h.win, h.n), 100*fairness, 100*fairSE)
				for _, ch := range []struct {
					trust   float64
					smart   bool
					minLead float64
				}{
					{1.0, false, 0}, {0.5, false, 0}, {0.2, false, 0}, {0, true, humanLead},
				} {
					cc := withCheat(base, rl.role, ch.trust, ch.smart, ch.minLead)
					cs := run(cc, *rounds, *seed, key)
					gain := sign * (cs.win - h.win)
					gSE := diffSE(cs.win, cs.n, h.win, h.n)
					row += fmt.Sprintf(" | %+.1f%%p ± %.1f%%p", 100*gain, 100*gSE)
				}
				fmt.Println(row + " |")
			}
		}
	}

	// ---- B. SmartWallhack: 사람(최소 예고 시간 있음) vs 기계(트리거봇, 한계 없음)
	fmt.Printf("\n## B. SmartWallhack 사람(최소 예고 %.0fms) vs 기계(한계 없음) (지연 %.0fms, 라운드 %d개)\n\n", *humanLeadMs, *latMs, *rounds)
	fmt.Println("| 모드 | 선공개(ms) | 치트 역할 | 기계 이득 | 사람 이득 |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	for mi, md := range modes {
		for li, look := range looks {
			key := uint64(10000 + mi*100 + li)
			base := gateCfg(md.mode, look, lat)
			h := run(base, *rounds, *seed, key)
			for _, rl := range roles {
				sign := 1.0
				if rl.role == duel.RolePeeker {
					sign = -1.0
				}
				machine := run(withCheat(base, rl.role, 0, true, 0), *rounds, *seed, key)
				human := run(withCheat(base, rl.role, 0, true, humanLead), *rounds, *seed, key)
				mg, hg := sign*(machine.win-h.win), sign*(human.win-h.win)
				fmt.Printf("| %s | %.0f | %s | %+.1f%%p ± %.1f%%p | %+.1f%%p ± %.1f%%p |\n",
					md.name, look, rl.name, 100*mg, 100*diffSE(machine.win, machine.n, h.win, h.n),
					100*hg, 100*diffSE(human.win, human.n, h.win, h.n))
			}
		}
	}

	// ---- C. 렌더링 가정 민감도: 보간(Interp) vs 외삽(Extrapolate) 클라이언트에서 ViewerOnly의 공정성
	fmt.Printf("\n## C. 렌더링 가정 민감도 — 정직 대 정직만, 공정성(게이트 끔 대비) (지연 %.0fms, 라운드 %d개)\n\n", *latMs, *renderRounds)
	fmt.Println("| 모드 | 선공개(ms) | 렌더링 | 정직 대 정직 승률 | 공정성(게이트 끔 대비) |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	offR := run(offBase, *renderRounds, *seed, 20000)
	for mi, md := range modes {
		for li, look := range looks {
			for ri, rd := range []struct {
				mode bot.Render
				name string
			}{{bot.Interp, "보간"}, {bot.Extrapolate, "외삽"}} {
				key := uint64(30000 + mi*1000 + li*10 + ri)
				cfg := gateCfg(md.mode, look, lat)
				cfg.Render = rd.mode
				s := run(cfg, *renderRounds, *seed, key)
				fairness := s.win - offR.win
				fmt.Printf("| %s | %.0f | %s | %.1f%% ± %.1f%%p | %+.1f%%p ± %.1f%%p |\n",
					md.name, look, rd.name, 100*s.win, 100*se95(s.win, s.n),
					100*fairness, 100*diffSE(s.win, s.n, offR.win, offR.n))
			}
		}
	}

	// ---- D. 발견: 무작위 미끼 쌍 빈도가 ViewerOnly에서 SmartWallhack에 새 구멍을 여는가
	//
	// 2.5단계는 무작위 미끼를 거울 쌍으로 보내라고(DecoyPairFreq=1.0) 권장했다. ViewerOnly에서는
	// 진짜 신호가 절대 쌍으로 안 뜨므로(홀더가 안 움직이면 soonVisible이 항상 거짓), SmartWallhack이
	// "쌍 신호"를 프라이밍 근거로 쓸 수 있는 유일한 원천이 무작위 미끼의 쌍이 돼버린다 — 그 쌍은
	// 실제 노출과 시간적으로 무관한데도(무작위 미끼는 자기 타이머로 독립적으로 뜬다), 프라이밍
	// 로직은 "최근에 쌍을 봤다"는 사실만 본다. 그 결과 쌍 빈도를 올릴수록 ViewerOnly·홀더 역할에서
	// SmartWallhack 이득이 되살아난다 — Both 모드(진짜 신호 자신의 쌍이 항상 있어서 영향 없음, 2.5단계
	// C 표)와 달리 ViewerOnly는 쌍 빈도에 민감하다.
	fmt.Printf("\n## D. 무작위 미끼 쌍 빈도가 ViewerOnly에 새로 여는 구멍 (홀더 치트, 선공개 %.0fms, 지연 %.0fms, 라운드 %d개)\n\n", looks[1], *latMs, *rounds)
	fmt.Println("| 모드 | 쌍 빈도 | SmartWallhack(사람) 이득 |")
	fmt.Println("| --- | --- | --- |")
	for mi, md := range modes {
		for pi, pf := range []float64{0, 0.5, 1.0} {
			key := uint64(40000 + mi*100 + pi)
			base := gateCfg(md.mode, looks[1], lat)
			base.DecoyPairFreq = pf
			h := run(base, *rounds, *seed, key)
			cc := withCheat(base, duel.RoleHolder, 0, true, humanLead)
			cs := run(cc, *rounds, *seed, key)
			gain := cs.win - h.win
			fmt.Printf("| %s | %.0f%% | %+.1f%%p ± %.1f%%p |\n",
				md.name, 100*pf, 100*gain, 100*diffSE(cs.win, cs.n, h.win, h.n))
		}
	}
}
