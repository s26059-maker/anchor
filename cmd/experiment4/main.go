// 실험 4: 2.7단계 — (1) 선공개 = 지연은 원리인가(지터·적응형 선공개), (2) ViewerOnly에서 홀더 쪽 미끼는
// 필요 없는가, (3) 미끼 안전 불변식, (4) 피커 채널 증거 미끼.
//
//	go run ./cmd/experiment4
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"

	"github.com/s26059-maker/anchor/internal/bot"
	"github.com/s26059-maker/anchor/internal/duel"
	"github.com/s26059-maker/anchor/internal/evidence"
	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/netsim"
)

func newRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, key*0x9e3779b97f4a7c15+1))
}
func newDecoyRng(seed, key uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed^0xD3C0FEED, key*0x9e3779b97f4a7c15+1))
}

// outcome: 한 설정의 라운드별 결과. 라운드 i는 같은 (seed,key)의 다른 설정과 같은 시나리오 시작점을 공유한다(짝지음).
type outcome struct {
	win     []int8 // 1 홀더 승, 0 홀더 패, -1 시간 초과
	popin   []float32
	leak    []float32
	pev     []int8
	shown   []bool
	retract []bool
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
		o.popin = append(o.popin, float32(res.PopIn))
		o.leak = append(o.leak, float32(res.Leak))
		o.pev = append(o.pev, int8(res.PeekerEv))
		o.shown = append(o.shown, res.PDShown)
		o.retract = append(o.retract, res.PDRetract)
	}
	return o
}

// execAll: 설정들을 병렬로 돌린다. 각 설정은 자기 rng만 쓰므로 결과는 병렬도와 무관하게 같다.
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

func winRate(o *outcome) (float64, int) {
	w, n := 0, 0
	for _, v := range o.win {
		if v >= 0 {
			n++
			w += int(v)
		}
	}
	if n == 0 {
		return 0, 0
	}
	return float64(w) / float64(n), n
}

// diff: a와 b의 홀더 승률 차이(a − b)와 95% CI 반폭. 두 설정 모두 판정이 난 라운드의 짝지은 차이로 계산한다.
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

func pp(x float64) string { return fmt.Sprintf("%+.1f", 100*x) }
func ppci(m, h float64) string {
	return fmt.Sprintf("%+.1f ± %.1f", 100*m, 100*h)
}

// cheat: 치트 전략 하나.
type cheat struct {
	name    string
	trust   float64
	smart   bool
	minLead float64
	geo     float64
	verify  int
}

func withCheat(base duel.Config, role duel.Role, c cheat) duel.Config {
	x := base
	x.CheatRole, x.Cheat, x.Trust, x.Smart, x.MinLead, x.GeoWindow, x.VerifyTicks = role, true, c.trust, c.smart, c.minLead, c.geo, c.verify
	return x
}

// gain: 치트를 쓰는 쪽 자신의 승률 증가분(피커가 치트면 홀더 승률 감소분).
func gain(role duel.Role, cheatO, honest *outcome) (float64, float64) {
	m, h := diff(cheatO, honest)
	if role == duel.RolePeeker {
		m = -m
	}
	return m, h
}

func wilsonUpper(x, n int) float64 {
	if n == 0 {
		return 1
	}
	const z = 1.96
	p := float64(x) / float64(n)
	den := 1 + z*z/float64(n)
	c := p + z*z/(2*float64(n))
	w := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n)*float64(n)))
	return math.Min(1, (c+w)/den)
}

var jitters = []struct {
	name string
	sd   float64
	sp   float64
	ss   float64
}{
	{"없음", 0, 0, 0},
	{"σ=5ms", 5, 0, 0},
	{"σ=10ms", 10, 0, 0},
	{"σ=5ms+스파이크(2%,+60ms)", 5, 0.02, 60},
}

func model(mean, sd, sp, ss float64) *netsim.Model {
	return &netsim.Model{Mean: mean / 1000, Jitter: sd / 1000, SpikeProb: sp, SpikeSize: ss / 1000}
}

func gcfg(mode gate.Mode, net *netsim.Model, lookMs float64) duel.Config {
	return duel.Config{
		Gate:    gate.Gate{Enabled: true, Mode: mode, Lookahead: lookMs / 1000, Samples: 8},
		Latency: net.Mean, Net: net, Holds: true,
	}
}

func adaptive(mode gate.Mode, net *netsim.Model, marginMs float64) duel.Config {
	c := gcfg(mode, net, 0)
	c.Adaptive, c.Margin = true, marginMs/1000
	return c
}

func offCfg(net *netsim.Model) duel.Config {
	return duel.Config{Latency: net.Mean, Net: net, Holds: true}
}

var peekerCheats = []cheat{{"신뢰100%", 1, false, 0, 0, 0}, {"신뢰50%", 0.5, false, 0, 0, 0}, {"신뢰20%", 0.2, false, 0, 0, 0}, {"Smart(기계)", 0, true, 0, 0, 0}}

// row: 한 정책의 정직 결과 + 피커 치트 4종.
type row struct {
	label  string
	honest *outcome
	lossM  float64
	lossH  float64
	popMs  float64
	leakMs float64
	gainM  []float64
	gainH  []float64
	lossOK bool
}

func main() {
	rounds := flag.Int("rounds", 20000, "설정당 라운드 수")
	latMs := flag.Float64("latency", 20, "평균 편도 지연(ms)")
	seed := flag.Uint64("seed", 13, "난수 시드")
	maxLoss := flag.Float64("maxloss", 1.0, "공정성 손실 허용치(%p)")
	flag.Parse()
	R := *rounds

	runRows := func(off *outcome, labels []string, cfgs []duel.Config, key uint64) []row {
		var specs []spec
		for _, c := range cfgs {
			specs = append(specs, spec{c, R, *seed, key})
			for _, ch := range peekerCheats {
				specs = append(specs, spec{withCheat(c, duel.RolePeeker, ch), R, *seed, key})
			}
		}
		outs := execAll(specs)
		var rows []row
		k := 1 + len(peekerCheats)
		for i := range cfgs {
			rw := row{label: labels[i], honest: outs[i*k]}
			rw.lossM, rw.lossH = diff(rw.honest, off)
			rw.popMs, rw.leakMs = 1000*meanNonNeg(rw.honest.popin), 1000*meanNonNeg(rw.honest.leak)
			for _, co := range outs[i*k+1 : (i+1)*k] {
				m, h := gain(duel.RolePeeker, co, rw.honest)
				rw.gainM, rw.gainH = append(rw.gainM, m), append(rw.gainH, h)
			}
			rw.lossOK = 100*rw.lossM <= *maxLoss
			rows = append(rows, rw)
		}
		return rows
	}
	header := func(first string) {
		fmt.Printf("| %s | 공정성 손실(%%p) | 팝인 평균(ms) | 누출 평균(ms) |", first)
		for _, ch := range peekerCheats {
			fmt.Printf(" 피커 %s 이득(%%p) |", ch.name)
		}
		fmt.Println()
		fmt.Print("| --- | --- | --- | --- |")
		for range peekerCheats {
			fmt.Print(" --- |")
		}
		fmt.Println()
	}
	printRow := func(rw row) {
		fmt.Printf("| %s | %s | %.1f | %.1f |", rw.label, ppci(rw.lossM, rw.lossH), rw.popMs, rw.leakMs)
		for i := range rw.gainM {
			fmt.Printf(" %s |", ppci(rw.gainM[i], rw.gainH[i]))
		}
		fmt.Println()
	}

	// ================= 0. 원리 검증: 필요한 선공개 = 지연
	fmt.Printf("## 0. 원리 검증 — 고정 지연 d에서 선공개 L 스캔 (ViewerOnly, 지터 없음, 라운드 %d개)\n\n", R)
	fmt.Println("공정성 손실 = 게이트 끔 대비 정직 홀더 승률 증가분(피커가 늦게 보는 만큼 홀더가 이득). 누출 = 피커에게 홀더 정보가 실제 노출보다 미리 도착한 평균 시간.")
	fmt.Println()
	header("d(ms) / 선공개")
	for di, d := range []float64{10, 20, 40, 60} {
		net := model(d, 0, 0, 0)
		off := exec(spec{offCfg(net), R, *seed, uint64(di)})
		var labels []string
		var cfgs []duel.Config
		for _, dl := range []float64{-10, -5, 0, 10, 30} {
			L := d + dl
			if L < 0 {
				continue
			}
			labels = append(labels, fmt.Sprintf("d=%.0f, L=%.0f (L−d=%+.0f)", d, L, dl))
			cfgs = append(cfgs, gcfg(gate.ViewerOnly, net, L))
		}
		labels = append(labels, fmt.Sprintf("d=%.0f, 적응형 m=0", d))
		cfgs = append(cfgs, adaptive(gate.ViewerOnly, net, 0))
		for _, rw := range runRows(off, labels, cfgs, uint64(di)) {
			printRow(rw)
		}
	}

	// ================= A. 지터 × 여유분 m, 고정 vs 적응형
	fmt.Printf("\n## A. 지터 × 선공개 정책 — 공정성 손실과 피커 누출 (ViewerOnly, 평균 지연 %.0fms, 라운드 %d개, 짝지은 난수)\n", *latMs, R)
	baseMargins := []float64{0, 5, 10, 20, 30}
	baseFixed := []float64{20, 30, 40, 50, 70}
	type best struct {
		jit      string
		ad, fx   *row
		adV, fxV float64
	}
	var bests []best
	for ji, j := range jitters {
		net := model(*latMs, j.sd, j.sp, j.ss)
		margins, fixed := baseMargins, baseFixed
		if j.sp > 0 { // 스파이크는 격자를 넘어서는 여유가 필요해 격자를 늘린다
			margins = append(append([]float64{}, baseMargins...), 45, 60)
			fixed = append(append([]float64{}, baseFixed...), 90, 120)
		}
		off := exec(spec{offCfg(net), R, *seed, uint64(100 + ji)})
		offP, _ := winRate(off)
		fmt.Printf("\n### 지터: %s (게이트 끔 정직 홀더 승률 %.1f%%)\n\n", j.name, 100*offP)
		header("선공개")
		var labels []string
		var cfgs []duel.Config
		for _, L := range fixed {
			labels = append(labels, fmt.Sprintf("고정 %.0fms", L))
			cfgs = append(cfgs, gcfg(gate.ViewerOnly, net, L))
		}
		for _, m := range margins {
			labels = append(labels, fmt.Sprintf("적응형 m=%.0fms", m))
			cfgs = append(cfgs, adaptive(gate.ViewerOnly, net, m))
		}
		rows := runRows(off, labels, cfgs, uint64(100+ji))
		for _, rw := range rows {
			printRow(rw)
		}
		b := best{jit: j.name}
		for i := range fixed {
			if rows[i].lossOK && b.fx == nil {
				b.fx, b.fxV = &rows[i], fixed[i]
			}
		}
		for i := range margins {
			if rows[len(fixed)+i].lossOK && b.ad == nil {
				b.ad, b.adV = &rows[len(fixed)+i], margins[i]
			}
		}
		bests = append(bests, b)
	}

	// ================= B. 공정성 손실 ≤ maxloss 를 지키는 최소 정책에서의 누출
	fmt.Printf("\n## B. 공정성 손실 ≤ %.1f%%p를 지키는 최소 정책과 그때의 피커 누출\n\n", *maxLoss)
	fmt.Println("| 지터 | 정책 | 공정성 손실(pp) | 누출 평균(ms) | 피커 신뢰100% 이득(pp) | 신뢰50% | 신뢰20% | Smart(기계) |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")
	pr := func(jit, pol string, rw *row) {
		if rw == nil {
			fmt.Printf("| %s | %s | (격자 안에 없음) | | | | | |\n", jit, pol)
			return
		}
		fmt.Printf("| %s | %s | %s | %.1f |", jit, pol, ppci(rw.lossM, rw.lossH), rw.leakMs)
		for i := range rw.gainM {
			fmt.Printf(" %s |", ppci(rw.gainM[i], rw.gainH[i]))
		}
		fmt.Println()
	}
	for _, b := range bests {
		if b.ad != nil {
			pr(b.jit, fmt.Sprintf("적응형 최소 m=%.0fms", b.adV), b.ad)
		} else {
			pr(b.jit, "적응형", nil)
		}
		if b.fx != nil {
			pr(b.jit, fmt.Sprintf("고정 최소 L=%.0fms", b.fxV), b.fx)
		} else {
			pr(b.jit, "고정", nil)
		}
	}

	// ================= C. 주장 2: ViewerOnly에서 홀더 쪽 미끼는 필요 없다
	fmt.Printf("\n## C. 홀더 미끼 끔/켬 × 렌더링 — 공정성과 홀더 치트 전략 이득 (라운드 %d개)\n\n", R)
	fmt.Println("미끼 켬 = 2.5단계 그대로(거울 미끼 + 무작위 미끼 + 핸들 섞기, 쌍 빈도 0%, 진짜 속도 떼기). 외삽은 제한 없음(exp3과 같은 조건).")
	fmt.Println("공정성 손실은 두 기준으로 잰다: ①같은 렌더링의 게이트 끔 대비(같은 클라이언트가 게이트 없이 받는 경험), ②보간·게이트 끔 대비(2.6단계의 기준).")
	fmt.Println()
	holderCheats := []cheat{{"신뢰100%", 1, false, 0, 0, 0}, {"신뢰50%", 0.5, false, 0, 0, 0}, {"신뢰20%", 0.2, false, 0, 0, 0}, {"신뢰5%", 0.05, false, 0, 0, 0}, {"Smart(사람100ms)", 0, true, 0.1, 0, 0}, {"Smart(기계)", 0, true, 0, 0, 0}}
	fmt.Print("| 조건 | 모드 | 선공개(ms) | 홀더 미끼 | 렌더링 | 손실① 같은 렌더링 끔 대비(pp) | 손실② 보간 끔 대비(pp) |")
	for _, ch := range holderCheats {
		fmt.Printf(" %s 이득 |", ch.name)
	}
	fmt.Println(" 최대 이득 상한(pp) |")
	fmt.Print("| --- | --- | --- | --- | --- | --- | --- |")
	for range holderCheats {
		fmt.Print(" --- |")
	}
	fmt.Println(" --- |")
	type cvar struct {
		name string
		net  *netsim.Model
		adp  bool
	}
	cnets := []cvar{{"고정 20ms", model(*latMs, 0, 0, 0), false}, {"σ=10ms + 적응형", model(*latMs, 10, 0, 0), true}}
	type cmode struct {
		m      gate.Mode
		name   string
		decoys []bool
	}
	cmodes := []cmode{{gate.ViewerOnly, "ViewerOnly", []bool{true, false}}, {gate.Both, "Both", []bool{false}}}
	for ci, cv := range cnets {
		key := uint64(200 + ci)
		offI := exec(spec{offCfg(cv.net), R, *seed, key})
		offXc := offCfg(cv.net)
		offXc.Render = bot.Extrapolate
		offX := exec(spec{offXc, R, *seed, key})
		for _, md := range cmodes {
			for _, decoys := range md.decoys {
				for _, rd := range []struct {
					mode bot.Render
					name string
				}{{bot.Interp, "보간"}, {bot.Extrapolate, "외삽"}} {
					looks := []float64{20, 50}
					if cv.adp {
						looks = []float64{-1}
					}
					for _, L := range looks {
						c := gcfg(md.m, cv.net, max(L, 0))
						look := fmt.Sprintf("%.0f", L)
						if cv.adp {
							c = adaptive(md.m, cv.net, 20)
							look = "적응(m=20)"
						}
						c.Render, c.Decoys = rd.mode, decoys
						specs := []spec{{c, R, *seed, key}}
						for _, ch := range holderCheats {
							specs = append(specs, spec{withCheat(c, duel.RoleHolder, ch), R, *seed, key})
						}
						outs := execAll(specs)
						same := offI
						if rd.mode == bot.Extrapolate {
							same = offX
						}
						f1m, f1h := diff(outs[0], same)
						f2m, f2h := diff(outs[0], offI)
						fmt.Printf("| %s | %s | %s | %s | %s | %s | %s |", cv.name, md.name, look, map[bool]string{true: "켬", false: "끔"}[decoys], rd.name, ppci(f1m, f1h), ppci(f2m, f2h))
						top := math.Inf(-1)
						for _, o := range outs[1:] {
							g, h := gain(duel.RoleHolder, o, outs[0])
							fmt.Printf(" %s |", ppci(g, h))
							top = math.Max(top, g+h)
						}
						fmt.Printf(" %s |\n", pp(top))
					}
				}
			}
		}
	}

	// ================= D. 안전 불변식(decoy.Guard)과 외삽 파국
	fmt.Printf("\n## D. 미끼 안전 불변식과 외삽 파국 진단 (지연 %.0fms 고정, 선공개 20ms, 외삽 클라이언트, 라운드 %d개)\n\n", *latMs, R)
	fmt.Println("공정성 손실은 외삽 렌더링·게이트 끔·미끼 없음 기준. 불변식은 외삽 한도 0.15초 동안 viewer와 LOS가 안 생기는 미끼만 보낸다(viewer 최대 속력 5m/s 방향 전부 포함).")
	fmt.Println("이득은 홀더 치트 전략(같은 설정의 정직 홀더 대비).")
	fmt.Println()
	fmt.Print("| 모드 | 핸들 섞기의 진짜 속도 | 불변식 | 외삽 한도 | 공정성 손실(pp) |")
	for _, ch := range []string{"신뢰100%", "신뢰50%", "신뢰20%", "Smart(기계)"} {
		fmt.Printf(" %s 이득 |", ch)
	}
	fmt.Println()
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	dnet := model(*latMs, 0, 0, 0)
	offx := offCfg(dnet)
	offx.Render = bot.Extrapolate
	offxO := exec(spec{offx, R, *seed, 300})
	dcheats := []cheat{holderCheats[0], holderCheats[1], holderCheats[2], holderCheats[5]}
	type dvar struct {
		md    gate.Mode
		mname string
		keep  bool
		guard bool
		limit float64
	}
	var dvars []dvar
	for _, md := range []struct {
		m    gate.Mode
		name string
	}{{gate.Both, "Both"}, {gate.ViewerOnly, "ViewerOnly"}} {
		for _, keep := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				dvars = append(dvars, dvar{md.m, md.name, keep, guard, 0.15})
			}
		}
		dvars = append(dvars, dvar{md.m, md.name, false, false, 0}) // 기존 조건(exp3 C 표의 미끼 켬 행)
	}
	var dspecs []spec
	for _, v := range dvars {
		c := gcfg(v.md, dnet, 20)
		c.Render, c.Decoys, c.MixKeepVel, c.DecoyGuard, c.MaxExtrap = bot.Extrapolate, true, v.keep, v.guard, v.limit
		c.GuardViewerMaxSpeed = duel.RunSpeed
		dspecs = append(dspecs, spec{c, R, *seed, 300})
		for _, ch := range dcheats {
			dspecs = append(dspecs, spec{withCheat(c, duel.RoleHolder, ch), R, *seed, 300})
		}
	}
	douts := execAll(dspecs)
	for i, v := range dvars {
		o := douts[i*(1+len(dcheats)) : (i+1)*(1+len(dcheats))]
		m, h := diff(o[0], offxO)
		lim := "없음(기존)"
		if v.limit > 0 {
			lim = "0.15s"
		}
		fmt.Printf("| %s | %s | %s | %s | %s |", v.mname, map[bool]string{false: "뗌(기존)", true: "유지"}[v.keep], map[bool]string{false: "끔", true: "켬"}[v.guard], lim, ppci(m, h))
		for _, co := range o[1:] {
			g, gh := gain(duel.RoleHolder, co, o[0])
			fmt.Printf(" %s |", ppci(g, gh))
		}
		fmt.Println()
	}

	// ================= E. 피커 채널 증거 미끼
	peekerDecoyTable(R, *seed, *latMs)
}

func peekerDecoyTable(R int, seed uint64, latMs float64) {
	net := model(latMs, 0, 0, 0)
	const key = 400
	base := gcfg(gate.ViewerOnly, net, 50) // 선공개 50 > 지연 20: 피커 채널이 실제로 새는 설정
	base.MaxExtrap = 0.15
	far := base // 먼 미끼: 노출과 거리가 먼 자리에 가만히
	far.PeekerDecoy, far.PeekerDecoyLen, far.DecoyGuard, far.GuardViewerMaxSpeed = true, 0.1, true, duel.RunSpeed
	farNoGuard := far
	farNoGuard.DecoyGuard = false
	band := far // 띠 미끼: 노출 시각이 (불변식 창, 선공개) 사이
	band.PeekerBand, band.GuardWindow, band.PeekerDecoyLen = true, 0.03, 0.03
	bandLoose := band // 불변식 창을 지연보다 짧게 잡으면 화면에 뜬다
	bandLoose.GuardWindow = 0.005

	cheats := []cheat{{"신뢰100%", 1, false, 0, 0, 0}, {"신뢰50%", 0.5, false, 0, 0, 0}, {"신뢰20%", 0.2, false, 0, 0, 0},
		{"Smart(기계)", 0, true, 0, 0, 0}, {"기하필터(신뢰100%, 창 0.15s)", 1, false, 0, 0.15, 0}, {"기하필터+검증3틱", 1, false, 0, 0.15, 3}}

	type item struct {
		name string
		cfg  duel.Config
	}
	items := []item{{"미끼 없음", base}, {"먼 미끼 + 불변식(0.15s)", far}, {"먼 미끼, 불변식 끔", farNoGuard}, {"띠 미끼 + 불변식(0.03s)", band}, {"띠 미끼, 불변식 창 5ms(지연보다 짧음)", bandLoose}}
	var specs []spec
	for _, it := range items {
		specs = append(specs, spec{it.cfg, R, seed, key})
		for _, ch := range cheats {
			specs = append(specs, spec{withCheat(it.cfg, duel.RolePeeker, ch), R, seed, key})
		}
	}
	outs := execAll(specs)
	k := 1 + len(cheats)
	honestNoFake := outs[0]

	fmt.Printf("\n## E. 피커 채널 증거 미끼(가짜 홀더) — ViewerOnly, 선공개 50ms, 지연 %.0fms 고정, 라운드 %d개\n\n", latMs, R)
	count := func(o *outcome) (shown, retract, valid, hit int) {
		for i := range o.pev {
			if o.shown[i] {
				shown++
			}
			if o.retract[i] {
				retract++
			}
			if o.pev[i] >= 0 {
				valid++
				if o.pev[i] == 1 {
					hit++
				}
			}
		}
		return
	}
	fmt.Println("| 설정 | 미끼 표시율 | 도중 거둔 비율 | 정직 피커 공정성 변화(pp, 미끼 없음 대비) | 정직 반응률 p₀ (95% 상한) |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	p0 := map[int]float64{}
	for i, it := range items {
		if i == 0 {
			fmt.Printf("| %s | - | - | - | - |\n", it.name)
			continue
		}
		o := outs[i*k]
		sh, rt, valid, hit := count(o)
		m, h := diff(o, honestNoFake)
		up := wilsonUpper(hit, valid)
		p0[i] = up
		fmt.Printf("| %s | %.1f%% | %.1f%% | %s | %d/%d = %.2f%% (상한 %.2f%%) |\n", it.name, 100*float64(sh)/float64(R), 100*float64(rt)/float64(R), ppci(m, h),
			hit, valid, 100*float64(hit)/math.Max(1, float64(valid)), 100*up)
	}

	fmt.Println("\n치터의 미끼 반응률과 이득 (이득 = 피커 치트 승률 증가분, 같은 설정의 정직 피커 대비):")
	fmt.Println()
	fmt.Println("| 미끼 설정 | 치트 전략 | 미끼 반응률 | 이득(pp) | E≥1e9까지 중앙값 관측 수 (미끼가 뜬 라운드 기준) |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	for i, it := range items {
		if i == 0 {
			continue
		}
		for j, ch := range cheats {
			o := outs[i*k+1+j]
			_, _, valid, hit := count(o)
			g, h := gain(duel.RolePeeker, o, outs[i*k])
			q := float64(hit) / math.Max(1, float64(valid))
			fmt.Printf("| %s | %s | %d/%d = %.1f%% | %s | %s |\n", it.name, ch.name, hit, valid, 100*q, ppci(g, h), obsToConvict(p0[i], q))
		}
	}
	fmt.Println("\n참고 — 미끼 없는 설정의 치트 이득(같은 조건, 위 이득 열과 비교):")
	fmt.Println()
	fmt.Println("| 치트 전략 | 이득(pp) |")
	fmt.Println("| --- | --- |")
	for j, ch := range cheats {
		g, h := gain(duel.RolePeeker, outs[1+j], outs[0])
		fmt.Printf("| %s | %s |\n", ch.name, ppci(g, h))
	}
}

// obsToConvict: 정직 기저율 p0(상한)로 만든 대안 혼합 e-value가 반응률 q인 계정에서 1e9에 닿기까지 관측 수 중앙값.
func obsToConvict(p0, q float64) string {
	if q <= p0 {
		return "∞ (반응률 ≤ p₀)"
	}
	const accounts, maxObs = 400, 5000
	r := rand.New(rand.NewPCG(7, 7))
	var ns []int
	for a := 0; a < accounts; a++ {
		ev := evidence.NewMixture(p0, 40)
		n := 0
		for n < maxObs {
			ev.Observe(r.Float64() < q)
			n++
			if ev.Over(1e-9) {
				break
			}
		}
		if n >= maxObs {
			n = math.MaxInt
		}
		ns = append(ns, n)
	}
	sort.Ints(ns)
	if ns[len(ns)/2] == math.MaxInt {
		return fmt.Sprintf(">%d", maxObs)
	}
	return fmt.Sprintf("%d", ns[len(ns)/2])
}
