package pathtrace

import "math"

// Limits describes what the drivetrain can physically do. Defaults are sane for
// a goBILDA mecanum base; override them once and the estimate tracks reality.
type Limits struct {
	// TopSpeed at maxPower 1.0, inches/sec.
	TopSpeed float64
	// Accel and Decel, inches/sec^2.
	Accel float64
	Decel float64
	// LatAccel is the sideways grip budget, inches/sec^2. It is what forces the
	// robot to slow for a tight curve rather than pretending corners are free.
	LatAccel float64
}

func DefaultLimits() Limits {
	return Limits{TopSpeed: 55, Accel: 80, Decel: 90, LatAccel: 70}
}

// Profile computes a speed at every point of every curve, plus per-segment
// length, peak speed and estimated duration.
//
// It is the standard forward/backward sweep: cap by maxPower and by curvature,
// then make the result reachable given the acceleration limit, then make it
// stoppable given the deceleration limit. Each segment starts and ends at rest,
// because blob settles onto every target before the auto advances.
func (t *Trace) Profile(lim Limits) {
	for i := range t.Segments {
		seg := &t.Segments[i]
		seg.Speeds, seg.Length, seg.EstSeconds, seg.PeakSpeed = profileCurve(seg.Curve, seg.MaxPower, lim)
	}
}

func profileCurve(curve [][]float64, maxPower float64, lim Limits) (speeds []float64, length, seconds, peak float64) {
	n := len(curve)
	if n < 2 {
		return []float64{0}, 0, 0, 0
	}

	// Arc length between consecutive samples.
	ds := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		ds[i] = math.Hypot(curve[i+1][0]-curve[i][0], curve[i+1][1]-curve[i][1])
		length += ds[i]
	}
	if length <= 1e-9 {
		return make([]float64, n), 0, 0, 0
	}

	vCap := lim.TopSpeed * clamp(maxPower, 0, 1)
	if vCap <= 0 {
		return make([]float64, n), length, 0, 0
	}

	// Ceiling at each point: the power cap, tightened by how hard the curve bends.
	v := make([]float64, n)
	for i := 0; i < n; i++ {
		v[i] = vCap
		if k := curvature(curve, i); k > 1e-6 {
			if corner := math.Sqrt(lim.LatAccel / k); corner < v[i] {
				v[i] = corner
			}
		}
	}

	// Start and end at rest.
	v[0] = 0
	v[n-1] = 0

	// Forward: you cannot be going faster than you could have accelerated to.
	for i := 1; i < n; i++ {
		reachable := math.Sqrt(v[i-1]*v[i-1] + 2*lim.Accel*ds[i-1])
		v[i] = math.Min(v[i], reachable)
	}

	// Backward: you cannot be going faster than you could still stop from.
	for i := n - 2; i >= 0; i-- {
		stoppable := math.Sqrt(v[i+1]*v[i+1] + 2*lim.Decel*ds[i])
		v[i] = math.Min(v[i], stoppable)
	}

	for i := 0; i < n-1; i++ {
		avg := (v[i] + v[i+1]) / 2
		if avg > 1e-6 {
			seconds += ds[i] / avg
		}
		if v[i] > peak {
			peak = v[i]
		}
	}
	if v[n-1] > peak {
		peak = v[n-1]
	}

	return v, length, seconds, peak
}

// curvature at a point, from the circle through it and its two neighbours.
// 1/R = 4*area / (a*b*c), which degenerates to 0 for collinear points.
func curvature(pts [][]float64, i int) float64 {
	if i == 0 || i >= len(pts)-1 {
		return 0
	}
	ax, ay := pts[i-1][0], pts[i-1][1]
	bx, by := pts[i][0], pts[i][1]
	cx, cy := pts[i+1][0], pts[i+1][1]

	a := math.Hypot(bx-ax, by-ay)
	b := math.Hypot(cx-bx, cy-by)
	c := math.Hypot(cx-ax, cy-ay)
	if a < 1e-9 || b < 1e-9 || c < 1e-9 {
		return 0
	}

	area := math.Abs((bx-ax)*(cy-ay)-(cx-ax)*(by-ay)) / 2
	if area < 1e-12 {
		return 0
	}

	return 4 * area / (a * b * c)
}

// Totals returns estimated and measured durations, in seconds.
func (t *Trace) Totals() (estimated, actual float64) {
	for _, s := range t.Segments {
		estimated += s.EstSeconds
		actual += s.ActualSeconds(t.DurationMs)
	}
	return estimated, actual
}

// SpeedRange is the span of profiled speeds, used to scale the colour ramp.
func (t *Trace) SpeedRange() (lo, hi float64) {
	hi = 0
	for _, s := range t.Segments {
		if s.PeakSpeed > hi {
			hi = s.PeakSpeed
		}
	}
	if hi <= 0 {
		hi = 1
	}
	return 0, hi
}

// Bounds is the field extent covered by the trace, padded a little.
func (t *Trace) Bounds() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)

	for _, s := range t.Segments {
		for _, p := range s.Curve {
			minX = math.Min(minX, p[0])
			maxX = math.Max(maxX, p[0])
			minY = math.Min(minY, p[1])
			maxY = math.Max(maxY, p[1])
		}
	}
	if math.IsInf(minX, 1) {
		return -72, -72, 72, 72
	}

	pad := 8.0
	return minX - pad, minY - pad, maxX + pad, maxY + pad
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
