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
	TickRate  = 128
	MaxTime   = 6.0
	RunSpeed  = 5.0  // m/s
	PeekX     = -1.5 // 피커가 다니는 x
	ExitY     = 6.45 // |y|가 이보다 크면 홀더에게 보인다
	EvidWin   = 0.4  // 미끼 등장 후 이 시간 안에 에임이 미끼 출구로 가면 "반응"
	EvidTol   = 3.0  // 반응으로 보는 각도 오차(도)
	HoldEvery = 1.5  // 정직한 홀더가 지키는 각도를 바꾸는 평균 주기(초)
)

type Config struct {
	Cheat   bool    // 홀더가 월핵을 쓰는가
	Trust   float64 // 월핵이 벽 뒤 신호를 믿고 움직일 확률
	Gate    gate.Gate
	Latency float64 // 편도 지연(초)
	Decoys  bool    // 미끼(거울 미끼 + 무작위 미끼 + 핸들 섞기)
	Holds   bool    // 홀더가 할 일 없을 때 위·가운데·아래 각도를 번갈아 지킴
}

type Result struct {
	HolderWon bool
	Timeout   bool
	InfoLead  float64 // 실제로 보이기 몇 초 전에 홀더에게 피커 위치가 전송됐나
	DecoyR    int     // 무작위 미끼에 대한 반응: -1 판정 불가, 0 무반응, 1 반응
}

func human(r *rand.Rand, aim float64, cheat bool, trust float64) *bot.Human {
	return &bot.Human{
		Rng:        r,
		UsesHidden: cheat,
		Trust:      trust,
		Reaction:   max(0.12, 0.20+0.03*r.NormFloat64()),
		Omega:      max(300, 600+100*r.NormFloat64()),
		Settle:     0.04,
		Anticipate: 0.12,
		Tol:        1.0,
		Forget:     0.25,
		Aim:        aim,
	}
}

func Round(cfg Config, r *rand.Rand, st *gate.Stats) Result {
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

	hb := human(r, 180, cfg.Cheat, cfg.Trust) // 벽 가운데를 보고 시작
	if cfg.Holds {
		hb.Holds = []float64{exitAngle(1), 180, exitAngle(-1)}
		hb.HoldEvery = HoldEvery
	}
	brains := map[int]*bot.Human{
		holder.ID: hb,
		peeker.ID: human(r, peeker.Pos.AngleTo(holder.Pos)+abs(5+2*r.NormFloat64()), false, 0),
	}
	lines := map[int]*netsim.DelayLine{
		holder.ID: {Delay: cfg.Latency},
		peeker.ID: {Delay: cfg.Latency},
	}
	g := cfg.Gate
	dt := 1.0 / TickRate
	tLOS, tInfo := -1.0, -1.0

	// 미끼 준비
	var mix *decoy.Mixer
	var mirrorH, randH int
	dSide := 1.0
	if r.IntN(2) == 0 {
		dSide = -1
	}
	dStart := 0.3 + 2.5*r.Float64()
	dLen := math.Max(g.Lookahead, dt)
	if cfg.Decoys {
		mix = decoy.NewMixer(r)
		mirrorH, randH = mix.NewHandle(), mix.NewHandle()
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
			snap := g.Filter(w, p, s)
			if p == holder {
				if tInfo < 0 && len(snap.Enemies) > 0 {
					tInfo = now
				}
				if cfg.Decoys {
					var fakes []gate.EnemyView
					// 거울 미끼: 선공개 중인 진짜 적의 반대편 출구에 똑같이 움직이는 가짜
					for _, e := range snap.Enemies {
						if !m.LOS(holder.Pos, e.Pos) {
							fakes = append(fakes, gate.EnemyView{ID: mirrorH, Pos: geom.Vec{X: e.Pos.X, Y: -e.Pos.Y}})
						}
					}
					// 무작위 미끼: 아무 때나 출구 직전까지 왔다가 사라지는 가짜 (증거 수집용)
					if now >= dStart && now < dStart+dLen {
						y := dSide * (ExitY - RunSpeed*(dStart+dLen-now))
						fakes = append(fakes, gate.EnemyView{ID: randH, Pos: geom.Vec{X: PeekX, Y: y}})
						if !evOn {
							evOn, evT0 = true, now
							evPre = math.Abs(geom.AngleDiff(hb.Aim, exitAngle(dSide))) <= EvidTol
						}
					}
					snap.Enemies = mix.Mix(snap.Enemies, fakes)
				}
			}
			lines[p.ID].Push(snap)
		}
		// 클라이언트: 각자 자기 화면(지연된 스냅샷)만 보고 행동. 같은 틱 동시 사격은 순서를 섞는다
		order := []*world.Player{holder, peeker}
		if r.IntN(2) == 0 {
			order[0], order[1] = order[1], order[0]
		}
		for _, p := range order {
			fire, tid := brains[p.ID].Update(now, dt, p, lines[p.ID].View(now), m)
			if !fire {
				continue
			}
			if p == holder && cfg.Decoys {
				tid = mix.RealOf(tid) // 미끼를 쏘면 -1 → 아무 일도 없음
			}
			t := w.ByID(tid)
			if t != nil && m.LOS(p.Pos, t.Pos) { // 판정은 서버 진실로
				return Result{HolderWon: p == holder, InfoLead: tLOS - tInfo, DecoyR: decoyR()}
			}
		}
		// 증거: 미끼가 뜬 뒤 EvidWin 안에 홀더 에임이 미끼 출구로 갔나
		if evOn && !evPre && now <= evT0+EvidWin &&
			math.Abs(geom.AngleDiff(hb.Aim, exitAngle(dSide))) <= EvidTol {
			evHit = true
		}
		w.Step(dt)
	}
	return Result{Timeout: true, DecoyR: decoyR()}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
