// Package duel: "코너 피크" 1대1 라운드. 월핵이 가장 큰 이득을 보는 상황이다.
//
//	홀더(20,0)가 벽(x=0, y=-6~6)을 지키고, 피커가 벽 뒤(x=-1.5)에서
//	위 또는 아래로 무작위 시점에 튀어나온다. 홀더는 어느 쪽인지 모른다. 월핵이 아니라면.
package duel

import (
	"math"
	"math/rand/v2"

	"github.com/s26059-maker/anchor/internal/bot"
	"github.com/s26059-maker/anchor/internal/decoy"
	"github.com/s26059-maker/anchor/internal/gate"
	"github.com/s26059-maker/anchor/internal/geom"
	"github.com/s26059-maker/anchor/internal/netsim"
	"github.com/s26059-maker/anchor/internal/world"
)

const (
	TickRate      = 128
	MaxTime       = 6.0
	RunSpeed      = 5.0  // m/s
	PeekX         = -1.5 // 피커가 다니는 x
	ExitY         = 6.45 // |y|가 이보다 크면 홀더에게 보인다
	EvidWin       = 0.4  // 미끼 등장 후 이 시간 안에 에임이 미끼 출구로 가면 "반응"
	EvidTol       = 3.0  // 반응으로 보는 각도 오차(도)
	HoldEvery     = 1.5  // 정직한 홀더가 지키는 각도를 바꾸는 평균 주기(초)
	SmartPrimeWin = 1.0  // SmartWallhack: 쌍 신호가 끊긴 뒤 반응 타이머를 버리기까지(초)
	FastReactThr  = 0.05 // 이 값(초)보다 빨리 에임이 정렬되면 "비정상으로 빠름"
)

// Role: 누가 월핵을 쓰는가.
type Role int

const (
	RoleHolder Role = iota // 기본값. 기존 동작과 같다
	RolePeeker
)

type Config struct {
	Cheat         bool       // CheatRole 쪽이 월핵을 쓰는가
	Trust         float64    // 월핵이 벽 뒤 신호를 믿고 움직일 확률
	Smart         bool       // SmartWallhack: 신호 개수로 미끼를 거르고, 쌍 신호는 타이밍(방향 아님)에만 쓴다
	CheatRole     Role       // 홀더가 치트를 쓰는가(기본), 피커가 쓰는가
	MinLead       float64    // SmartWallhack 사람 한계: 쌍 신호가 이만큼(초) 앞서 안 뜨면 못 써먹는다(0=기계)
	Render        bot.Render // 클라이언트가 받은 위치를 그대로 그리는가(Interp), 외삽하는가(Extrapolate)
	Gate          gate.Gate
	Latency       float64 // 편도 지연(초)
	Decoys        bool    // 미끼(거울 미끼 + 무작위 미끼 + 핸들 섞기). 홀더가 받는 스트림에만 적용된다
	Holds         bool    // 홀더가 할 일 없을 때 위·가운데·아래 각도를 번갈아 지킴
	DecoyPairFreq float64 // 무작위 미끼를 거울 쌍으로 보낼 확률 (0=항상 혼자, 1=항상 쌍)

	// ---- 2.7단계 (전부 기본값이면 기존 동작과 같다)

	// Net: 지터·스파이크가 있는 지연 모델. nil이면 고정 Latency. 플레이어마다 독립 링크(하행+ack 상행)를 뽑는다.
	Net *netsim.Model
	// Adaptive: 선공개 = 서버가 잰 지연 추정(최근 MeterWindow개 ack 왕복의 절반 평균) + Margin.
	// Gate.Lookahead는 무시한다. 서버는 실제 지연을 모르고 측정값만 안다.
	Adaptive    bool
	Margin      float64 // 적응형 선공개의 여유분(초)
	MeterWindow int     // 측정 이동 평균 창 (0이면 16)
	// MaxExtrap: 외삽 클라이언트가 받은 스냅샷 시각 이후로 외삽하는 최대 시간(초). 0이면 제한 없음.
	MaxExtrap float64
	// DecoyGuard: 모든 미끼(홀더·피커 스트림)에 안전 불변식을 강제한다(decoy.Guard). MaxExtrap이 0이면 0.15초로 본다.
	DecoyGuard          bool
	GuardViewerMaxSpeed float64 // 0이면 viewer의 최대 속력을 안 본다
	// MixKeepVel: 핸들 섞기에서 진짜 적의 속도를 떼지 않는다(기존은 떼고 위치만 보냄).
	MixKeepVel bool
	// PeekerDecoy: 피커 스트림에 가짜 홀더(증거용)를 PeekerDecoyLen초 동안 띄운다. 불변식을 어기는 순간 거둔다.
	PeekerDecoy    bool
	PeekerDecoyLen float64
	// PeekerBand: 피커 미끼를 "띠" 안에 둔다. 피커가 벽 끝을 향해 달릴 때, 가짜 홀더의 노출 시각이
	// (GuardWindow, 선공개) 사이가 되도록 위치를 계산해 띄운다. 진짜 신호도 노출 선공개 전에 뜨므로
	// 노출 임박 여부로는 진짜와 구분되지 않는다. 불변식 창(GuardWindow) 밖이라 화면에는 안 뜬다.
	PeekerBand bool
	// GuardWindow: >0이면 불변식이 이 시간(초)만 본다(기본은 MaxExtrap). 보간 클라이언트는
	// 미끼가 화면에 머무는 시간(지연 + 한 틱)만 덮으면 된다. 서버가 아는 건 지연 추정뿐이라
	// 지연 스파이크가 이 창을 넘기면 불변식이 깨질 수 있다.
	GuardWindow float64
	// GeoWindow: 기하 필터 월핵(>0). 치터가 노출 임박이 아닌 벽 뒤 신호를 버린다. bot.Human.GeoWindow 참고.
	GeoWindow float64
	// VerifyTicks: 검증형 월핵(>0). 벽 뒤 신호가 이 틱 수만큼 연속으로 떠 있어야 믿는다. bot.Human.VerifyTicks 참고.
	VerifyTicks int
}

type Result struct {
	HolderWon bool
	Timeout   bool
	InfoLead  float64 // 실제로 보이기 몇 초 전에 홀더에게 피커 위치가 전송됐나
	DecoyR    int     // 무작위 미끼에 대한 반응: -1 판정 불가, 0 무반응, 1 반응
	FastR     int     // 쌍 신호 뒤 "너무 빠른" 에임 정렬: -1 판정 불가, 0 정상, 1 비정상으로 빠름

	// 2.7단계. 피커 화면 기준: 홀더가 실제로 보이는 시각(tLOS) 대비 피커에게 홀더 정보가 도착한 시각.
	PopIn     float64 // 늦게 뜬 시간(초, ≥0). 정보가 tLOS 뒤에 도착했으면 그 차이. 노출이 없었으면 -1
	Leak      float64 // 미리 도착한 시간(초, ≥0). 정보가 tLOS보다 앞서 도착했으면 그 차이. 노출이 없었으면 -1
	PeekerEv  int     // 피커 미끼에 대한 반응: -1 판정 불가(미끼 안 뜸/이미 정렬), 0 무반응, 1 반응
	PDShown   bool    // 피커 미끼가 한 틱이라도 전송됐나
	PDRetract bool    // 피커 미끼가 불변식 때문에 도중에 거둬졌나
}

func human(r *rand.Rand, aim float64, cheat bool, trust float64, smart bool, minLead float64, render bot.Render) *bot.Human {
	return &bot.Human{
		Rng:         r,
		UsesHidden:  cheat,
		Trust:       trust,
		Smart:       smart,
		PrimeWindow: SmartPrimeWin,
		MinLead:     minLead,
		RenderMode:  render,
		Reaction:    max(0.12, 0.20+0.03*r.NormFloat64()),
		Omega:       max(300, 600+100*r.NormFloat64()),
		Settle:      0.04,
		Anticipate:  0.12,
		Tol:         1.0,
		Forget:      0.25,
		Aim:         aim,
	}
}

// Round: 한 라운드를 시뮬레이션한다.
// r은 시나리오용 rng(피커 방향, 대기 시간, 사람 파라미터) — 미끼 유무와 무관하게 같은 시드면 같은 라운드를 만든다.
// decoyRng는 미끼 전용 rng(무작위 미끼 방향·시작 시각·쌍 여부, 핸들 발급, 순서 섞기) — cfg.Decoys가 false면 건드리지 않는다.
func Round(cfg Config, r *rand.Rand, decoyRng *rand.Rand, st *gate.Stats) Result {
	// 지연 링크용 시드. 지터 모델을 쓸 때만 r을 한 번 소비한다(같은 시드의 모든 설정이 똑같이 소비 → 짝지음 유지).
	var netSeed uint64
	if netModel(cfg) != nil {
		netSeed = r.Uint64()
	}
	m := &world.Map{Walls: []geom.Segment{{A: geom.Vec{X: 0, Y: -6}, B: geom.Vec{X: 0, Y: 6}}}}
	holder := &world.Player{ID: 1, Team: 0, Pos: geom.Vec{X: 20, Y: 0}, Alive: true}
	peeker := &world.Player{ID: 2, Team: 1, Pos: geom.Vec{X: PeekX, Y: 0}, Alive: true}
	w := &world.World{Map: m, Players: []*world.Player{holder, peeker}}
	exitAngle := func(side float64) float64 { return holder.Pos.AngleTo(geom.Vec{X: PeekX, Y: side * ExitY}) }

	side := 1.0
	if r.IntN(2) == 0 {
		side = -1
	}
	wait := 0.3 + 2.2*r.Float64()

	holderCheat, holderTrust, holderSmart := false, 0.0, false
	peekerCheat, peekerTrust, peekerSmart := false, 0.0, false
	if cfg.CheatRole == RolePeeker {
		peekerCheat, peekerTrust, peekerSmart = cfg.Cheat, cfg.Trust, cfg.Smart
	} else {
		holderCheat, holderTrust, holderSmart = cfg.Cheat, cfg.Trust, cfg.Smart
	}

	hb := human(r, 180, holderCheat, holderTrust, holderSmart, cfg.MinLead, cfg.Render) // 벽 가운데를 보고 시작
	if cfg.Holds {
		hb.Holds = []float64{exitAngle(1), 180, exitAngle(-1)}
		hb.HoldEvery = HoldEvery
	}
	hb.MaxExtrap, hb.GeoWindow, hb.VerifyTicks = cfg.MaxExtrap, cfg.GeoWindow, cfg.VerifyTicks
	pb := human(r, peeker.Pos.AngleTo(holder.Pos)+abs(5+2*r.NormFloat64()), peekerCheat, peekerTrust, peekerSmart, cfg.MinLead, cfg.Render)
	pb.MaxExtrap, pb.GeoWindow, pb.VerifyTicks = cfg.MaxExtrap, cfg.GeoWindow, cfg.VerifyTicks
	brains := map[int]*bot.Human{
		holder.ID: hb,
		peeker.ID: pb,
	}
	cheaterBrain, cheaterPlayer := hb, holder
	if cfg.CheatRole == RolePeeker {
		cheaterBrain, cheaterPlayer = pb, peeker
	}
	lines := map[int]*netsim.DelayLine{
		holder.ID: {Delay: cfg.Latency},
		peeker.ID: {Delay: cfg.Latency},
	}
	if net := netModel(cfg); net != nil {
		for _, p := range []*world.Player{holder, peeker} {
			dl := lines[p.ID]
			dl.Down = netsim.NewLink(*net, netSeed, uint64(p.ID)*2)
			dl.Up = netsim.NewLink(*net, netSeed, uint64(p.ID)*2+1)
			dl.Meter = &netsim.Meter{Window: cfg.MeterWindow, Initial: net.Mean}
		}
	}
	g := cfg.Gate
	dt := 1.0 / TickRate
	tLOS, tInfo := -1.0, -1.0

	// 미끼 준비 (전용 rng: decoyRng). cfg.Decoys가 꺼져 있으면 decoyRng를 전혀 건드리지 않는다 —
	// 시나리오(r)는 미끼 유무와 무관하게 항상 같은 순서로 소비된다.
	var mix *decoy.Mixer
	var mirrorH, randH, randH2 int
	var dSide, dStart, dLen float64
	pairDecoy := false
	if cfg.Decoys {
		dSide = 1.0
		if decoyRng.IntN(2) == 0 {
			dSide = -1
		}
		dStart = 0.3 + 2.5*decoyRng.Float64()
		dLen = math.Max(g.Lookahead, dt)
		pairDecoy = decoyRng.Float64() < cfg.DecoyPairFreq
		mix = decoy.NewMixer(decoyRng)
		mix.KeepVel = cfg.MixKeepVel
		mirrorH, randH = mix.NewHandle(), mix.NewHandle()
		if pairDecoy {
			randH2 = mix.NewHandle()
		}
	}
	evOn, evPre, evHit := false, false, false
	evT0 := 0.0
	decoyR := func() int {
		if !evOn || evPre {
			return -1
		}
		if evHit {
			return 1
		}
		return 0
	}
	reactLatched := false
	fastR := -1

	// 2.7단계: 안전 불변식과 피커 미끼. cfg.PeekerDecoy가 꺼져 있으면 decoyRng를 건드리지 않는다.
	var guard *decoy.Guard
	if cfg.DecoyGuard {
		me := cfg.MaxExtrap
		if me <= 0 {
			me = 0.15
		}
		if cfg.GuardWindow > 0 {
			me = cfg.GuardWindow
		}
		guard = &decoy.Guard{Map: m, MaxExtrap: me, ViewerMaxSpeed: cfg.GuardViewerMaxSpeed}
	}
	var pmix *decoy.Mixer
	var pdHandle int
	var pdStart, pdEnd float64
	var pdPos geom.Vec
	pdShown, pdDead, pdPre, pdHit := false, false, false, false
	pdT0, pdAim0, pdTrig := 0.0, 0.0, 0.0
	if cfg.PeekerDecoy {
		pdSide := 1.0
		if decoyRng.IntN(2) == 0 {
			pdSide = -1
		}
		pdPos = geom.Vec{X: holder.Pos.X, Y: pdSide * (7 + 5*decoyRng.Float64())}
		pdStart = 0.3 + 2.5*decoyRng.Float64()
		pdEnd = pdStart + math.Max(cfg.PeekerDecoyLen, dt)
		if cfg.PeekerBand {
			pdStart, pdEnd = math.Inf(1), 0 // 띠 미끼는 피커가 달리기 시작해 트리거가 걸릴 때까지 꺼둔다
		}
		pdTrig = 4.0 + 1.0*decoyRng.Float64() // 띠 미끼: 피커가 벽 끝에 이만큼 다가오면 띄운다
		pmix = decoy.NewMixer(decoyRng)
		pmix.KeepVel = true
		pdHandle = pmix.NewHandle()
	}
	tInfoP, tArrP := -1.0, -1.0
	finish := func(res Result) Result {
		res.PopIn, res.Leak, res.PeekerEv = -1, -1, -1
		if tLOS >= 0 && tArrP >= 0 {
			res.PopIn, res.Leak = math.Max(0, tArrP-tLOS), math.Max(0, tLOS-tArrP)
		}
		res.PDShown, res.PDRetract = pdShown, pdDead
		if pdShown && !pdPre {
			res.PeekerEv = 0
			if pdHit {
				res.PeekerEv = 1
			}
		}
		return res
	}

	for w.Time < MaxTime {
		now := w.Time
		if now >= wait {
			peeker.Vel = geom.Vec{X: 0, Y: side * RunSpeed}
		}
		// 서버: 진실 기록 + 게이트 통과한 스냅샷만 전송
		if tLOS < 0 && m.LOS(holder.Pos, peeker.Pos) {
			tLOS = now
		}
		for _, p := range w.Players {
			var s *gate.Stats
			if p == holder {
				s = st
			}
			gp := g
			if cfg.Adaptive {
				gp.Lookahead = lines[p.ID].Meter.Estimate(now) + cfg.Margin
			}
			snap := gp.Filter(w, p, s)
			if p == cheaterPlayer {
				if tInfo < 0 && len(snap.Enemies) > 0 {
					tInfo = now
				}
			}
			if p == holder {
				if cfg.Decoys {
					var mirrors, rands []gate.EnemyView
					// 거울 미끼: 선공개 중인 진짜 적의 반대편 출구에 똑같이 움직이는 가짜
					for _, e := range snap.Enemies {
						if !m.LOS(holder.Pos, e.Pos) {
							mirrors = append(mirrors, gate.EnemyView{ID: mirrorH, Pos: geom.Vec{X: e.Pos.X, Y: -e.Pos.Y}, Vel: geom.Vec{X: e.Vel.X, Y: -e.Vel.Y}})
						}
					}
					// 무작위 미끼: 아무 때나 출구 직전까지 왔다가 사라지는 가짜 (증거 수집용).
					// DecoyPairFreq 확률로 반대편에도 같은 모양의 가짜를 하나 더 보내 "쌍 = 진짜"라는
					// 개수 기반 추론을 무디게 만든다.
					if now >= dStart && now < dStart+dLen {
						mag := ExitY - RunSpeed*(dStart+dLen-now)
						rands = append(rands, gate.EnemyView{ID: randH, Pos: geom.Vec{X: PeekX, Y: dSide * mag}, Vel: geom.Vec{X: 0, Y: dSide * RunSpeed}})
						if pairDecoy {
							rands = append(rands, gate.EnemyView{ID: randH2, Pos: geom.Vec{X: PeekX, Y: -dSide * mag}, Vel: geom.Vec{X: 0, Y: -dSide * RunSpeed}})
						}
					}
					if guard != nil {
						mirrors, rands = guard.Filter(holder, mirrors), guard.Filter(holder, rands)
					}
					if len(rands) > 0 && !evOn {
						evOn, evT0 = true, now
						evPre = math.Abs(geom.AngleDiff(hb.Aim, exitAngle(dSide))) <= EvidTol
					}
					fakes := append(mirrors, rands...)
					snap.Enemies = mix.Mix(snap.Enemies, fakes)
				}
			}
			realInfo := p == peeker && tInfoP < 0 && len(snap.Enemies) > 0 // 미끼를 섞기 전 진짜 정보
			if p == peeker && cfg.PeekerDecoy {
				// 피커 스트림의 가짜 홀더: 피커가 스스로 언제 보일지 정하므로, 불변식이 깨지는 순간 거둔다(다시 안 보낸다).
				var fakes []gate.EnemyView
				if cfg.PeekerBand && !pdShown && !pdDead && peeker.Vel.Y != 0 && tLOS < 0 {
					sgn := math.Copysign(1, peeker.Vel.Y)
					if sgn*peeker.Pos.Y >= pdTrig {
						// 노출 시각이 (GuardWindow, 선공개) 사이가 되도록: te 뒤 피커 위치에서 시선이 벽 끝을 스치는 자리
						te := (cfg.GuardWindow + gp.Lookahead) / 2
						pe := peeker.Pos.Add(peeker.Vel.Scale(te))
						yf := pe.Y + (sgn*6-pe.Y)*(holder.Pos.X-pe.X)/(0-pe.X)
						pdPos = geom.Vec{X: holder.Pos.X, Y: yf}
						pdStart, pdEnd = now, now+math.Max(cfg.PeekerDecoyLen, dt)
					}
				}
				if now >= pdStart && now < pdEnd && !pdDead && tLOS < 0 { // 진짜 노출 뒤엔 미끼를 안 띄운다(반응 판정이 진짜와 섞인다)
					f := gate.EnemyView{ID: pdHandle, Pos: pdPos}
					if guard == nil || guard.Safe(peeker, f) {
						fakes = append(fakes, f)
						if !pdShown {
							pdShown, pdT0, pdAim0 = true, now, pb.Aim
							pdPre = math.Abs(geom.AngleDiff(pb.Aim, peeker.Pos.AngleTo(pdPos))) <= EvidTol
						}
					} else {
						pdDead = true
					}
				}
				snap.Enemies = pmix.Mix(snap.Enemies, fakes)
			}
			lines[p.ID].Push(snap)
			if realInfo {
				tInfoP, tArrP = now, lines[p.ID].LastArrival()
			}
		}
		// 클라이언트: 각자 자기 화면(지연된 스냅샷)만 보고 행동. 같은 틱 동시 사격은 순서를 섞는다
		order := []*world.Player{holder, peeker}
		if r.IntN(2) == 0 {
			order[0], order[1] = order[1], order[0]
		}
		for _, p := range order {
			fire, tid := brains[p.ID].Update(now, dt, p, lines[p.ID].View(now), m)
			if p == cheaterPlayer && !reactLatched && cheaterBrain.OnScreenAt() >= 0 && cheaterBrain.AimedAt() >= 0 {
				reactLatched = true
				if cheaterBrain.AimedAt()-cheaterBrain.OnScreenAt() < FastReactThr {
					fastR = 1
				} else {
					fastR = 0
				}
			}
			if !fire {
				continue
			}
			if p == holder && cfg.Decoys {
				tid = mix.RealOf(tid) // 미끼를 쏘면 -1 → 아무 일도 없음
			}
			if p == peeker && cfg.PeekerDecoy {
				tid = pmix.RealOf(tid)
			}
			t := w.ByID(tid)
			if t != nil && m.LOS(p.Pos, t.Pos) { // 판정은 서버 진실로
				return finish(Result{HolderWon: p == holder, InfoLead: tLOS - tInfo, DecoyR: decoyR(), FastR: fastR})
			}
		}
		// 증거: 미끼가 뜬 뒤 EvidWin 안에 홀더 에임이 미끼 출구로 갔나
		if evOn && !evPre && now <= evT0+EvidWin &&
			math.Abs(geom.AngleDiff(hb.Aim, exitAngle(dSide))) <= EvidTol {
			evHit = true
		}
		// 피커 미끼 증거: 뜬 뒤 EvidWin 안에 피커 에임이 미끼 방향으로 갔나
		// (에임이 실제로 움직여서 도착한 경우만 반응으로 본다 — 가만히 있다 우연히 방향이 겹친 정직 피커를 뺀다)
		if pdShown && !pdPre && now <= pdT0+EvidWin &&
			math.Abs(geom.AngleDiff(pb.Aim, peeker.Pos.AngleTo(pdPos))) <= EvidTol &&
			math.Abs(geom.AngleDiff(pb.Aim, pdAim0)) > EvidTol {
			pdHit = true
		}
		w.Step(dt)
	}
	return finish(Result{Timeout: true, DecoyR: decoyR(), FastR: fastR})
}

func netModel(cfg Config) *netsim.Model {
	if cfg.Net != nil {
		return cfg.Net
	}
	if cfg.Adaptive {
		return &netsim.Model{Mean: cfg.Latency}
	}
	return nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
