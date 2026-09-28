// Package netsim: 서버 스냅샷이 클라이언트 화면에 뜨기까지의 지연을 흉내 낸다.
package netsim

import "github.com/s26059-maker/anchor/internal/gate"

type DelayLine struct {
	Delay float64 // 편도 지연(초)
	queue []gate.Snapshot
	last  gate.Snapshot
}

func (d *DelayLine) Push(s gate.Snapshot) { d.queue = append(d.queue, s) }

// View: now 시점에 클라이언트가 가진 가장 최신 스냅샷.
func (d *DelayLine) View(now float64) gate.Snapshot {
	for len(d.queue) > 0 && d.queue[0].Time+d.Delay <= now+1e-9 {
		d.last = d.queue[0]
		d.queue = d.queue[1:]
	}
	return d.last
}
