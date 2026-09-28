// Package geom: 2D 벡터와 선분 교차. 단위는 미터.
package geom

import "math"

type Vec struct{ X, Y float64 }

func (a Vec) Add(b Vec) Vec       { return Vec{a.X + b.X, a.Y + b.Y} }
func (a Vec) Sub(b Vec) Vec       { return Vec{a.X - b.X, a.Y - b.Y} }
func (a Vec) Scale(k float64) Vec { return Vec{a.X * k, a.Y * k} }
func (a Vec) Len() float64        { return math.Hypot(a.X, a.Y) }

// AngleTo: a에서 b를 바라보는 방향(도).
func (a Vec) AngleTo(b Vec) float64 { return math.Atan2(b.Y-a.Y, b.X-a.X) * 180 / math.Pi }

type Segment struct{ A, B Vec }

func cross(o, a, b Vec) float64 { return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X) }

// Intersects: 선분 p1-p2가 벽 s를 가로지르면 true (끝점 접촉은 무시).
func Intersects(p1, p2 Vec, s Segment) bool {
	d1 := cross(s.A, s.B, p1)
	d2 := cross(s.A, s.B, p2)
	d3 := cross(p1, p2, s.A)
	d4 := cross(p1, p2, s.B)
	return d1*d2 < 0 && d3*d4 < 0
}

// AngleDiff: 각도 a에서 b로 가는 최소 회전량(도), -180~180.
func AngleDiff(a, b float64) float64 {
	return math.Mod(b-a+540, 360) - 180
}
