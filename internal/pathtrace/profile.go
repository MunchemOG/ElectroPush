package pathtrace

import "math"

// Limits is the drivetrain model speeds are estimated against.
type Limits struct {
	TopSpeed float64

	Accel float64
	Decel float64

	LatAccel float64
}

// DefaultLimits is a reasonable starting model for an FTC drivetrain.
func DefaultLimits() Limits {
	return Limits{TopSpeed: 55, Accel: 80, Decel: 90, LatAccel: 70}
}

// Profile estimates speed along a segment under the drivetrain limits.
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

	v := make([]float64, n)
	for i := 0; i < n; i++ {
		v[i] = vCap
		if k := curvature(curve, i); k > 1e-6 {
			if corner := math.Sqrt(lim.LatAccel / k); corner < v[i] {
				v[i] = corner
			}
		}
	}

	v[0] = 0
	v[n-1] = 0

	for i := 1; i < n; i++ {
		reachable := math.Sqrt(v[i-1]*v[i-1] + 2*lim.Accel*ds[i-1])
		v[i] = math.Min(v[i], reachable)
	}

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

// Totals is the estimated and measured duration of the whole run.
func (t *Trace) Totals() (estimated, actual float64) {
	for _, s := range t.Segments {
		estimated += s.EstSeconds
		actual += s.ActualSeconds(t.DurationMs)
	}
	return estimated, actual
}

// SpeedRange is the slowest and fastest modelled speed, for colouring.
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

// Bounds is the field area the run covers.
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
