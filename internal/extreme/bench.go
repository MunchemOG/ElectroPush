package extreme

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/hotreload"
)

// What a reload costs is three separate things, and lumping them together
// hides which one is worth attacking. Resolving the classpath is a Gradle
// invocation that could be cached. Compiling is javac and d8 on the laptop.
// Only the last part touches the robot.
//
// The robot side finishes within an event loop tick of the trigger being
// written, which is not separately measurable from here, so the total stops at
// the trigger and says so.

// Phase is one measured stage.
type Phase struct {
	Name    string
	Samples []time.Duration
}

// Best is the fastest sample, which is the fairest single figure for something
// this noisy.
func (p Phase) Best() time.Duration {
	if len(p.Samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), p.Samples...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	return sorted[0]
}

// Spread is the gap between fastest and slowest.
func (p Phase) Spread() time.Duration {
	if len(p.Samples) < 2 {
		return 0
	}
	sorted := append([]time.Duration(nil), p.Samples...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	return sorted[len(sorted)-1] - sorted[0]
}

// BenchResult is what a run of reloads measured.
type BenchResult struct {
	Classpath Phase
	Compile   Phase
	Deliver   Phase
	Total     Phase

	Runs    int
	Classes int
	Bridged int
	Bytes   int64

	Err error
}

// Benchmark times a reload, repeatedly.
//
// Each run does the whole thing, including resolving the classpath, because
// that is what a deploy does and quoting a number that skips a step nobody can
// skip would be misleading.
func Benchmark(p *Project, serial string, keep []string, runs int, progress func(string)) BenchResult {
	out := BenchResult{Runs: runs}

	out.Classpath.Name = "classpath (gradle)"
	out.Compile.Name = "compile (javac + d8)"
	out.Deliver.Name = "push and trigger"
	out.Total.Name = "total"

	report := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	for run := 0; run < runs; run++ {
		report(fmt.Sprintf("reload %d of %d", run+1, runs))

		started := time.Now()

		start := time.Now()
		cp, err := ResolveClasspath(p.Wrapper, Module)
		if err != nil {
			out.Err = err
			return out
		}
		out.Classpath.Samples = append(out.Classpath.Samples, time.Since(start))

		work, err := os.MkdirTemp("", "epsh-extreme-bench-*")
		if err != nil {
			out.Err = err
			return out
		}

		start = time.Now()
		build, err := Compile(p, cp, work, keep, RegisteredConfigs(serial))
		if err != nil {
			os.RemoveAll(work)
			out.Err = err
			return out
		}
		out.Compile.Samples = append(out.Compile.Samples, time.Since(start))
		out.Classes, out.Bridged = build.Classes, build.Bridged

		start = time.Now()
		delivery, err := hotreload.Deliver(serial, Name, build.Jar, build.Dex,
			time.Now().Format("150405"))
		if err != nil {
			os.RemoveAll(work)
			out.Err = err
			return out
		}
		out.Deliver.Samples = append(out.Deliver.Samples, time.Since(start))
		out.Bytes = delivery.Bytes

		out.Total.Samples = append(out.Total.Samples, time.Since(started))
		os.RemoveAll(work)
	}

	return out
}

// Report renders the measurement, in a shape that can go straight into a
// readme without being retyped.
func (r BenchResult) Report() string {
	var b strings.Builder

	b.WriteString("# Epsh Extreme reload\n\n")
	fmt.Fprintf(&b, "%s\n\n", time.Now().Format("2 January 2006, 15:04"))

	if r.Err != nil {
		fmt.Fprintf(&b, "Failed: %s\n", r.Err)
		return b.String()
	}

	fmt.Fprintf(&b, "%d classes, %d of them registered with FtcDashboard, %s sent.\n\n",
		r.Classes, r.Bridged, size(r.Bytes))

	fmt.Fprintf(&b, "| stage | best of %d | spread |\n|---|---|---|\n", r.Runs)
	for _, phase := range []Phase{r.Classpath, r.Compile, r.Deliver} {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", phase.Name, secs(phase.Best()), secs(phase.Spread()))
	}
	fmt.Fprintf(&b, "| **%s** | **%s** | %s |\n\n",
		r.Total.Name, secs(r.Total.Best()), secs(r.Total.Spread()))

	b.WriteString("Timed from the start of the command to the robot being told to reload.\n")
	b.WriteString("The robot picks it up on its next event loop tick, which is not separately\n")
	b.WriteString("measurable from here.\n\n")

	b.WriteString("Every run resolves the classpath, because a deploy does. That stage is the\n")
	b.WriteString("obvious thing to cache, and nothing has been cached yet.\n")

	return b.String()
}

func size(n int64) string {
	if n < 1024*1024 {
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func secs(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
