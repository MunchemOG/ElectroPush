package bench

import (
	"strings"
	"testing"
	"time"
)

func sample() (APK, []Run, Reload, map[string]bool) {
	apk := APK{
		Path:          "TeamCode-debug.apk",
		Size:          68 << 20,
		LibBytes:      24 << 20,
		LibPacked:     10 << 20,
		LibCompressed: true,
		DexBytes:      36 << 20,
		DexFiles:      25,
	}

	runs := []Run{
		{Name: "Android Studio equivalent", What: "baseline", Install: 40 * time.Second},
		{Name: "epsh, staged install", What: "staged", Install: 44 * time.Second},
		{Name: "epsh, streamed install", What: "streamed", Install: 32 * time.Second},
		{Name: "epsh, delta transfer", What: "delta", Install: 20 * time.Second},
		{Name: "epsh, delta + streamed", What: "both", Install: 18 * time.Second},
		{Name: "epsh, nothing changed", What: "skip", Install: 300 * time.Millisecond, Skipped: true},
	}

	reload := Reload{
		Measured:  true,
		DexBytes:  400 << 10,
		Push:      200 * time.Millisecond,
		Compile:   1500 * time.Millisecond,
		CompileOK: true,
		StubBytes: 1 << 10,
		Overhead:  190 * time.Millisecond,
	}

	settings := map[string]bool{"delta": true, "skip": true, "stream": true}

	return apk, runs, reload, settings
}

func TestReportCoversEverythingItPromises(t *testing.T) {
	report := Report(sample())

	for _, want := range []string{

		"24.0 MB", "36.0 MB", "25 files",

		"Android Studio equivalent", "delta + streamed", "nothing changed",

		"What each setting does", "Send only changed parts", "Skip install when unchanged",
		"Stream the install", "Store native libraries", "Install only changed splits",

		"Hot reload", "dex2oat",

		"Sloth", "not a Sloth replacement",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report never mentions %q", want)
		}
	}
}

func TestReportQuantifiesEachSetting(t *testing.T) {
	report := Report(sample())

	section := report[strings.Index(report, "What each setting does"):]

	if !strings.Contains(section, "faster") {
		t.Errorf("no setting is quantified as faster:\n%s", section)
	}

	if !strings.Contains(section, "would remove 24.0 MB of extraction") {
		t.Errorf("the stored-libraries effect is not quantified:\n%s", section)
	}

	if !strings.Contains(section, "300ms when nothing changed") {
		t.Errorf("the skip is not quantified:\n%s", section)
	}
}

func TestComparisonsAreRelativeToAndroidStudio(t *testing.T) {
	apk, runs, reload, settings := sample()
	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "2.2x faster") {
		t.Errorf("the best run is not compared to the baseline:\n%s", report)
	}

	if !strings.Contains(report, "slower") {
		t.Error("a configuration that lost is not reported as losing")
	}
}

func TestFailedRunsAppearInTheReport(t *testing.T) {
	apk, runs, reload, settings := sample()
	runs = append(runs, Run{Name: "epsh, changed split only", Err: errString("no splits")})

	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "failed") || !strings.Contains(report, "no splits") {
		t.Errorf("a failed run was hidden:\n%s", report)
	}
}

func TestAnUnmeasurableCompileIsSaidOutLoud(t *testing.T) {
	apk, runs, _, settings := sample()

	reload := Reload{
		Measured:   true,
		DexBytes:   400 << 10,
		Push:       200 * time.Millisecond,
		CompileWhy: "dex2oat is not available to the shell on this hub",
	}

	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "not available to the shell") {
		t.Errorf("the reason was dropped:\n%s", report)
	}
	if strings.Contains(report, "floor for a reload | 0s") {
		t.Error("an unmeasured compile was reported as zero")
	}
}

func TestSummaryOrdersByTime(t *testing.T) {
	_, runs, _, _ := sample()

	lines := strings.Split(strings.TrimSpace(Summary(runs)), "\n")
	if len(lines) != len(runs) {
		t.Fatalf("got %d lines for %d runs", len(lines), len(runs))
	}

	if !strings.Contains(lines[0], "nothing changed") {
		t.Errorf("the fastest run is not first: %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "staged") {
		t.Errorf("the slowest run is not last: %q", lines[len(lines)-1])
	}
}

func TestSizesReadAsPeopleWriteThem(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  string
	}{
		{0, "0"},
		{900, "900 bytes"},
		{512 << 10, "512 KB"},
		{1 << 20, "1.0 MB"},
		{68 << 20, "68.0 MB"},
	} {
		if got := mb(tc.bytes); got != tc.want {
			t.Errorf("mb(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}

	if got := secs(300 * time.Millisecond); got != "300ms" {
		t.Errorf("got %q", got)
	}
	if got := secs(90 * time.Second); got != "90.0s" {
		t.Errorf("got %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Running only half of `epsh dev` used to render the other half as a row of
// zeros, which reads as a measurement of nothing rather than as nothing
// measured. Both real reports hit this.
func TestSectionsThatWereNotRunSaySo(t *testing.T) {
	apk, runs, reload, settings := sample()

	t.Run("deploy only", func(t *testing.T) {
		report := Report(apk, runs, Reload{}, settings)

		if !strings.Contains(report, "Not run.") {
			t.Errorf("the unrun reload section does not say so:\n%s", report)
		}
		if strings.Contains(report, "floor for a reload | 0s") ||
			strings.Contains(report, "| dex used | 0 ") {
			t.Error("an unrun measurement was rendered as zeros")
		}
	})

	t.Run("reload only", func(t *testing.T) {
		report := Report(apk, nil, reload, settings)

		if !strings.Contains(report, "deploy benchmark was not run") {
			t.Errorf("the unrun deploy section does not say so:\n%s", report)
		}
		// This was the actively misleading one: nothing failed, it was never run.
		if strings.Contains(report, "baseline run failed") {
			t.Error("a benchmark that was never run is reported as failed")
		}
		if !strings.Contains(report, "needs the deploy benchmark") {
			t.Error("settings that need the benchmark do not say what is missing")
		}
		if !strings.Contains(report, "nothing to compare") {
			t.Error("the Sloth section invents a comparison with no runs")
		}
	})
}

// A 1 KB stub compiles in about the time dex2oat takes to start, so timing one
// measures startup and reports it as the cost of a reload.
func TestTheReloadSampleIsNotAStub(t *testing.T) {
	apk, runs, reload, settings := sample()
	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "smallest non-stub dex") {
		t.Error("the report does not say the sample avoids stubs")
	}
	// Startup and real work are separated, or 1.5s reads as all compile.
	if !strings.Contains(report, "of which startup") {
		t.Errorf("dex2oat startup is not separated out:\n%s", report)
	}
	if !strings.Contains(report, "190ms") {
		t.Error("the measured startup cost is missing")
	}
	// 1500ms total minus 190ms startup.
	if !strings.Contains(report, "1.3s") {
		t.Error("the marginal compile cost is missing")
	}
}

func TestMarginalNeverGoesNegative(t *testing.T) {
	r := Reload{Compile: 100 * time.Millisecond, Overhead: 190 * time.Millisecond}
	if got := r.Marginal(); got != 0 {
		t.Errorf("got %v, want 0", got)
	}
}

// Benchmarking without rebuilding measures the previous APK, and the setting
// then looks like it did nothing. That is what cost a real run.
func TestAStaleAPKIsCalledOut(t *testing.T) {
	apk, runs, reload, settings := sample()
	apk.Stale = true
	settings["storeLibs"] = true

	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "older than the project's gradle files") {
		t.Errorf("a stale APK is not flagged:\n%s", report)
	}
	if !strings.Contains(report, "ON BUT NOT BUILT") {
		t.Error("a build-time setting that has not been built is not called out")
	}
}

// A setting that is on and actually built must not be nagged about.
func TestABuiltSettingIsNotNagged(t *testing.T) {
	apk, runs, reload, settings := sample()
	apk.LibCompressed = false
	settings["storeLibs"] = true

	report := Report(apk, runs, reload, settings)

	if strings.Contains(report, "ON BUT NOT BUILT") {
		t.Error("a setting that is in the APK was reported as not built")
	}
	if !strings.Contains(report, "stored, so the install extracts nothing") {
		t.Error("the built state is not reported")
	}
}

// Single samples cannot tell a small difference from run-to-run variance, and
// reporting one as a finding is how a benchmark misleads.
func TestDifferencesInsideTheSpreadAreNotClaimed(t *testing.T) {
	apk, _, reload, settings := sample()

	runs := []Run{
		{Name: "Android Studio equivalent", Install: 43 * time.Second, Spread: 3 * time.Second, Samples: 3},
		{Name: "epsh, streamed install", Install: 44 * time.Second, Spread: 3 * time.Second, Samples: 3},
		{Name: "epsh, delta transfer", Install: 20 * time.Second, Spread: 2 * time.Second, Samples: 3},
	}

	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "within noise") {
		t.Errorf("a one-second gap on a three-second spread was claimed:\n%s", report)
	}
	// A real difference still has to be reported.
	if !strings.Contains(report, "faster") {
		t.Error("a difference well outside the spread was suppressed")
	}
	if !strings.Contains(report, "±") {
		t.Error("the spread is not shown next to the figures")
	}
}

func TestOneSampleSaysSo(t *testing.T) {
	apk, runs, reload, settings := sample()
	report := Report(apk, runs, reload, settings)

	if !strings.Contains(report, "One sample each") {
		t.Errorf("a single-sample run does not warn:\n%s", report)
	}
}
