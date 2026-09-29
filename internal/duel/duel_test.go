package duel

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/s26059-maker/anchor/internal/bot"
	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/netsim"
)

func outcomes(cfg Config, n int, seedA, seedB uint64) []int {
	r := rand.New(rand.NewPCG(seedA, seedB))
	dr := rand.New(rand.NewPCG(seedB, seedA))
	out := make([]int, n)
	for i := range out {
		res := Round(cfg, r, dr, nil)
		switch {
		case res.Timeout:
			out[i] = -1
		case res.HolderWon:
			out[i] = 1
		}
	}
	return out
}

// 2.7단계 이전(HEAD 2cd8297)의 코드로 뽑은 골든 값. 새 필드를 다 기본값으로 두면 기존 동작이 그대로여야 한다.
const goldenHead = `off role=0 smart=false wins=328 timeouts=0 lead=1077.960938
off role=0 smart=true wins=138 timeouts=0 lead=1076.875000
off role=1 smart=false wins=45 timeouts=0 lead=1081.109375
off role=1 smart=true wins=138 timeouts=0 lead=1076.875000
both20 role=0 smart=false wins=95 timeouts=0 lead=9.375000
both20 role=0 smart=true wins=150 timeouts=0 lead=9.375000
both20 role=1 smart=false wins=138 timeouts=0 lead=9.375000
both20 role=1 smart=true wins=138 timeouts=0 lead=9.375000
vo50 role=0 smart=false wins=119 timeouts=0 lead=0.000000
vo50 role=0 smart=true wins=138 timeouts=0 lead=0.000000
vo50 role=1 smart=false wins=83 timeouts=0 lead=21.875000
vo50 role=1 smart=true wins=138 timeouts=0 lead=21.875000
vo20extra role=0 smart=false wins=122 timeouts=0 lead=0.000000
vo20extra role=0 smart=true wins=122 timeouts=0 lead=0.000000
vo20extra role=1 smart=false wins=122 timeouts=0 lead=9.375000
vo20extra role=1 smart=true wins=122 timeouts=0 lead=9.375000
`

func TestLegacyBehaviorUnchanged(t *testing.T) {
	cfgs := map[string]Config{
		"off":       {Latency: 0.02, Holds: true},
		"both20":    {Gate: gate.Gate{Enabled: true, Lookahead: 0.02, Samples: 8}, Latency: 0.02, Holds: true, Decoys: true, DecoyPairFreq: 1},
		"vo50":      {Gate: gate.Gate{Enabled: true, Mode: gate.ViewerOnly, Lookahead: 0.05, Samples: 8}, Latency: 0.02, Holds: true, Decoys: true},
		"vo20extra": {Gate: gate.Gate{Enabled: true, Mode: gate.ViewerOnly, Lookahead: 0.02, Samples: 8}, Latency: 0.02, Holds: true, Decoys: true, Render: bot.Extrapolate},
	}
	var sb strings.Builder
	for _, name := range []string{"off", "both20", "vo50", "vo20extra"} {
		for _, role := range []Role{RoleHolder, RolePeeker} {
			for _, smart := range []bool{false, true} {
				c := cfgs[name]
				c.Cheat, c.Trust, c.Smart, c.CheatRole = true, 0.7, smart, role
				r := rand.New(rand.NewPCG(5, 6))
				dr := rand.New(rand.NewPCG(7, 8))
				h, to, lead := 0, 0, 0.0
				for i := 0; i < 400; i++ {
					res := Round(c, r, dr, nil)
					if res.HolderWon {
						h++
					}
					if res.Timeout {
						to++
					}
					lead += res.InfoLead
				}
				fmt.Fprintf(&sb, "%s role=%d smart=%v wins=%d timeouts=%d lead=%.6f\n", name, role, smart, h, to, lead)
			}
		}
	}
	if got := sb.String(); got != goldenHead {
		t.Fatalf("기존 설정의 결과가 바뀜:\n%s\n--- 기대 ---\n%s", got, goldenHead)
	}
}

func viewerOnly(net *netsim.Model, lookMs float64) Config {
	return Config{
		Gate:    gate.Gate{Enabled: true, Mode: gate.ViewerOnly, Lookahead: lookMs / 1000, Samples: 8},
		Latency: net.Mean, Net: net, Holds: true, MaxExtrap: 0.15,
	}
}

func same(a, b []int) int {
	d := 0
	for i := range a {
		if a[i] != b[i] {
			d++
		}
	}
	return d
}

// 불변식이 지켜지면 미끼는 정직한 피커에게 아무 영향이 없다: 미끼가 있든 없든 라운드 결과가 똑같다.
func TestPeekerDecoyInvisibleToHonest(t *testing.T) {
	net := &netsim.Model{Mean: 0.02}
	base := viewerOnly(net, 50)
	far := base
	far.PeekerDecoy, far.PeekerDecoyLen, far.DecoyGuard, far.GuardViewerMaxSpeed = true, 0.1, true, RunSpeed
	band := far
	band.PeekerBand, band.GuardWindow, band.PeekerDecoyLen = true, 0.03, 0.03
	loose := band
	loose.GuardWindow = 0.005
	ref := outcomes(base, 600, 1, 2)
	if d := same(ref, outcomes(far, 600, 1, 2)); d != 0 {
		t.Errorf("먼 미끼: 정직 결과가 %d라운드 달라짐", d)
	}
	if d := same(ref, outcomes(band, 600, 1, 2)); d != 0 {
		t.Errorf("띠 미끼: 정직 결과가 %d라운드 달라짐", d)
	}
	// 반대편(공허하지 않음): 불변식 창이 화면에 머무는 시간보다 짧으면 유령이 떠서 결과가 달라진다.
	if d := same(ref, outcomes(loose, 600, 1, 2)); d == 0 {
		t.Error("불변식 창을 지연보다 짧게 잡아도 결과가 안 변함 — 테스트가 공허하다")
	}
}

// 외삽 클라이언트에서 거울 미끼 + 핸들 섞기: 진짜 속도를 유지하고 불변식을 켜면 정직 결과가 게이트 끔과 똑같다.
func TestHolderDecoysGuardExtrapolationFair(t *testing.T) {
	net := &netsim.Model{Mean: 0.02}
	off := Config{Latency: 0.02, Net: net, Holds: true, Render: bot.Extrapolate}
	both := viewerOnly(net, 20)
	both.Gate.Mode = gate.Both
	both.Render, both.Decoys, both.MixKeepVel, both.DecoyGuard, both.GuardViewerMaxSpeed = bot.Extrapolate, true, true, true, RunSpeed
	if d := same(outcomes(off, 500, 3, 4), outcomes(both, 500, 3, 4)); d != 0 {
		t.Errorf("Both+미끼+불변식+외삽: 게이트 끔과 %d라운드 다름", d)
	}
	// 불변식을 끄면(그리고 속도를 떼면) 파국이 되살아난다.
	bad := both
	bad.DecoyGuard, bad.MixKeepVel = false, false
	if d := same(outcomes(off, 500, 3, 4), outcomes(bad, 500, 3, 4)); d < 50 {
		t.Errorf("불변식 없이도 결과가 거의 안 변함(%d) — 테스트가 공허하다", d)
	}
}

// 원리: 지터가 없을 때 선공개가 지연 이상이면 팝인이 없고, 미만이면 팝인이 생긴다. 적응형 m=0은 정확히 지연.
func TestLookaheadEqualsDelayPrinciple(t *testing.T) {
	net := &netsim.Model{Mean: 0.04}
	popin := func(c Config) (sum float64, n int) {
		r := rand.New(rand.NewPCG(9, 9))
		dr := rand.New(rand.NewPCG(9, 10))
		for i := 0; i < 300; i++ {
			res := Round(c, r, dr, nil)
			if res.PopIn >= 0 {
				sum += res.PopIn
				n++
			}
		}
		return
	}
	if s, n := popin(viewerOnly(net, 40)); n == 0 || s != 0 {
		t.Errorf("L=d인데 팝인 합 %v (n=%d)", s, n)
	}
	if s, _ := popin(viewerOnly(net, 30)); s == 0 {
		t.Error("L<d인데 팝인이 없음")
	}
	ad := viewerOnly(net, 0)
	ad.Adaptive = true
	if s, n := popin(ad); n == 0 || s != 0 {
		t.Errorf("적응형 m=0인데 팝인 합 %v (n=%d)", s, n)
	}
}

// 지터 모델을 켜도 라운드는 재현 가능하고, 같은 시드의 설정끼리 시나리오 시작점을 공유한다.
func TestJitterDeterministic(t *testing.T) {
	net := &netsim.Model{Mean: 0.02, Jitter: 0.01, SpikeProb: 0.02, SpikeSize: 0.06}
	c := viewerOnly(net, 30)
	if d := same(outcomes(c, 300, 5, 5), outcomes(c, 300, 5, 5)); d != 0 {
		t.Fatalf("같은 시드인데 %d라운드 다름", d)
	}
}

// 사격 등록 지연을 켜면 핑이 낮은 쪽이 이긴다: 피커 지연만 낮추면 피커 승률이 오른다.
func TestShotLatencyPingMatters(t *testing.T) {
	win := func(peekerMs float64) float64 {
		c := Config{Latency: 0.02, Net: &netsim.Model{Mean: 0.02}, PeekerNet: &netsim.Model{Mean: peekerMs / 1000}, Holds: true, ShotLatency: true}
		o := outcomes(c, 1500, 11, 12)
		h, n := 0, 0
		for _, v := range o {
			if v >= 0 {
				n++
				h += v
			}
		}
		return 1 - float64(h)/float64(n)
	}
	slow, fast := win(60), win(10)
	if fast <= slow+0.02 {
		t.Fatalf("피커 핑 10ms 승률 %.3f, 60ms 승률 %.3f — 핑이 승패에 영향이 없다", fast, slow)
	}
}
