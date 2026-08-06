package bench

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Sloth's published figures, quoted for context rather than measured here.
// Source: https://github.com/Dairy-Foundation/Sloth
const (
	SlothTypical = "under 1s"
	SlothCeiling = "about 2s"
	FastLoadTime = "about 7s"
	SlothVsFull  = "40s+"
)

// Report renders everything measured into something readable.
func Report(apk APK, runs []Run, reload Reload, settings map[string]bool) string {
	var b strings.Builder

	b.WriteString("# Pusher deploy report\n\n")
	fmt.Fprintf(&b, "%s\n\n", time.Now().Format("2 January 2006, 15:04"))

	writeAPK(&b, apk)
	writeRuns(&b, runs)
	writeSettingEffects(&b, apk, runs, settings)
	writeReload(&b, reload, runs)
	writeSloth(&b, runs)

	return b.String()
}

func writeAPK(b *strings.Builder, apk APK) {
	b.WriteString("## What is being deployed\n\n")

	if apk.Stale {
		b.WriteString("**This APK is older than the project's gradle files.** Anything below that\n")
		b.WriteString("depends on how the APK was built measures the previous build. Rebuild first.\n\n")
	}

	fmt.Fprintf(b, "| | |\n|---|---|\n")
	fmt.Fprintf(b, "| APK | %s |\n", mb(apk.Size))
	fmt.Fprintf(b, "| native libraries | %s in the APK, %s once extracted |\n",
		mb(apk.LibPacked), mb(apk.LibBytes))
	fmt.Fprintf(b, "| dex handed to dexopt | %s across %d files |\n", mb(apk.DexBytes), apk.DexFiles)

	packing := "stored, so the install does not extract them"
	if apk.LibCompressed {
		packing = fmt.Sprintf("compressed, so the install extracts %s", mb(apk.LibBytes))
	}
	if apk.Stale {
		packing += " (built before the current settings)"
	}
	fmt.Fprintf(b, "| library packing | %s |\n\n", packing)

	b.WriteString("The install is not just a copy. The package manager writes the APK into\n")
	b.WriteString("`/data/app`, verifies its signature, extracts the native libraries if they are\n")
	b.WriteString("compressed, and runs dexopt over every dex file. That is why it takes as long\n")
	b.WriteString("as it does, and it is why the settings below split into transfer and install.\n\n")
}

func writeRuns(b *strings.Builder, runs []Run) {
	b.WriteString("## Measured\n\n")

	if len(runs) == 0 {
		b.WriteString("The deploy benchmark was not run, so there is nothing here.\n")
		b.WriteString("Run it from `pusher dev` -> Benchmark the deploy.\n\n")
		return
	}

	b.WriteString("Each is a real deploy to the robot. The baseline is what Android Studio does:\n")
	b.WriteString("one streamed install of the whole APK, no delta, no skipping.\n\n")

	baseline := time.Duration(0)
	noise := time.Duration(0)
	single := false
	for _, run := range runs {
		if run.Err != nil || run.Skipped {
			continue
		}
		if strings.HasPrefix(run.Name, "Android Studio") {
			baseline = run.Total()
		}
		if run.Spread > noise {
			noise = run.Spread
		}
		if run.Samples <= 1 {
			single = true
		}
	}

	fmt.Fprintf(b, "| configuration | time | vs Android Studio | what it did |\n")
	fmt.Fprintf(b, "|---|---|---|---|\n")

	for _, run := range runs {
		if run.Err != nil {
			fmt.Fprintf(b, "| %s | failed | | %s |\n", run.Name, run.Err)
			continue
		}

		fmt.Fprintf(b, "| %s | %s | %s | %s |\n",
			run.Name, timing(run), compareWithin(run.Total(), baseline, noise), run.What)
	}

	b.WriteString("\n")

	if baseline == 0 {
		b.WriteString("The baseline run failed, so the comparisons above are blank.\n\n")
	}

	switch {
	case single:
		b.WriteString("One sample each. A deploy varies by seconds run to run, so treat anything\n")
		b.WriteString("closer than a few seconds as indistinguishable rather than as a result.\n\n")
	case noise > 0:
		fmt.Fprintf(b, "Best of %d each. The widest spread between repeats was %s, and differences\n",
			samplesOf(runs), secs(noise))
		b.WriteString("smaller than that are reported as noise rather than as a finding.\n\n")
	}
}

func samplesOf(runs []Run) int {
	for _, run := range runs {
		if run.Samples > 1 {
			return run.Samples
		}
	}
	return 1
}

// timing shows the spread alongside the figure, so a number is never read as
// more precise than it is.
func timing(run Run) string {
	if run.Spread <= 0 {
		return secs(run.Total())
	}
	return fmt.Sprintf("%s (±%s)", secs(run.Total()), secs(run.Spread))
}

func compare(got, baseline time.Duration) string {
	return compareWithin(got, baseline, 0)
}

// compareWithin refuses to call a difference that is smaller than the spread
// between repeats of the same configuration. A deploy varies by seconds, so a
// single pair of samples cannot tell a real 5% apart from noise.
func compareWithin(got, baseline, noise time.Duration) string {
	if baseline <= 0 || got <= 0 {
		return ""
	}

	gap := got - baseline
	if gap < 0 {
		gap = -gap
	}
	if noise > 0 && gap <= noise {
		return "within noise"
	}

	switch ratio := float64(baseline) / float64(got); {
	case ratio >= 1.05:
		return fmt.Sprintf("%.1fx faster", ratio)
	case ratio <= 0.95:
		return fmt.Sprintf("%.0f%% slower", (1/ratio-1)*100)
	default:
		return "about the same"
	}
}

func writeSettingEffects(b *strings.Builder, apk APK, runs []Run, settings map[string]bool) {
	b.WriteString("## What each setting does\n\n")
	b.WriteString("`pusher settings` -> Deploy speed\n\n")

	if len(runs) == 0 {
		b.WriteString("Only the settings that can be read off the APK are covered: the rest need\n")
		b.WriteString("the deploy benchmark, which was not run.\n\n")
	}

	find := func(name string) (Run, bool) {
		for _, run := range runs {
			if run.Name == name && run.Err == nil {
				return run, true
			}
		}
		return Run{}, false
	}

	staged, hasStaged := find("pusher, staged install")
	streamed, hasStreamed := find("pusher, streamed install")
	delta, hasDelta := find("pusher, delta transfer")
	skip, hasSkip := find("pusher, nothing changed")
	split, hasSplit := find("pusher, changed split only")

	fmt.Fprintf(b, "| setting | now | effect here |\n|---|---|---|\n")

	deltaEffect := "needs the deploy benchmark"
	if hasDelta && hasStaged {
		deltaEffect = fmt.Sprintf("%s vs %s staged (%s)",
			secs(delta.Total()), secs(staged.Total()), compare(delta.Total(), staged.Total()))
	}
	fmt.Fprintf(b, "| Send only changed parts | %s | %s |\n", onOff(settings["delta"]), deltaEffect)

	skipEffect := "needs the deploy benchmark"
	if hasSkip {
		skipEffect = fmt.Sprintf("%s when nothing changed", secs(skip.Total()))
		if !skip.Skipped {
			skipEffect = "did not trigger"
		}
	}
	fmt.Fprintf(b, "| Skip install when unchanged | %s | %s |\n", onOff(settings["skip"]), skipEffect)

	streamEffect := "needs the deploy benchmark"
	if hasStreamed && hasStaged {
		streamEffect = fmt.Sprintf("%s vs %s staged (%s)",
			secs(streamed.Total()), secs(staged.Total()), compare(streamed.Total(), staged.Total()))
	}
	fmt.Fprintf(b, "| Stream the install | %s | %s |\n", onOff(settings["stream"]), streamEffect)

	libsEffect := "stored, so the install extracts nothing"
	if apk.LibCompressed {
		libsEffect = fmt.Sprintf("would remove %s of extraction at install; APK grows by roughly %s",
			mb(apk.LibBytes), mb(apk.LibBytes-apk.LibPacked))
		if settings["storeLibs"] {
			// The setting is a build-time one. Benchmarking without rebuilding
			// measures the old APK and looks like the setting did nothing.
			libsEffect = "ON BUT NOT BUILT: this APK still has compressed libraries, so " +
				"nothing here measures it. Rebuild, then run again."
		}
	}
	fmt.Fprintf(b, "| Store native libraries | %s | %s |\n", onOff(settings["storeLibs"]), libsEffect)

	splitEffect := "the project builds one APK, so there is nothing to split"
	if hasSplit {
		splitEffect = secs(split.Total())
		if split.Skipped {
			splitEffect += " (no split had changed)"
		}
	}
	fmt.Fprintf(b, "| Install only changed splits | %s | %s |\n", onOff(settings["split"]), splitEffect)

	b.WriteString("\n")
	b.WriteString("Two of these are not free. Storing the libraries makes the APK bigger, which\n")
	b.WriteString("costs transfer time, so it is a win on USB or 5 GHz and a question on 2.4 GHz.\n")
	b.WriteString("Delta costs a little work on the hub to rebuild the APK, which is why it can\n")
	b.WriteString("lose over USB where the transfer was never the problem.\n\n")

	b.WriteString("Not swept here, because changing them needs a rebuild:\n\n")
	fmt.Fprintf(b, "- **One ABI** (`pusher slim`): the APK carries %s of native libraries. A stock\n", mb(apk.LibPacked))
	b.WriteString("  project packages two architectures and the hub runs one.\n")
	b.WriteString("- **Gradle threads**: build time only, nothing to do with the deploy.\n\n")
}

func writeReload(b *strings.Builder, reload Reload, runs []Run) {
	b.WriteString("## Hot reload, if it existed\n\n")

	if !reload.Measured {
		b.WriteString("Not run. `pusher dev` -> Hot reload feasibility measures it.\n\n")
		return
	}

	if reload.Err != nil {
		fmt.Fprintf(b, "Could not measure: %s\n\n", reload.Err)
		return
	}

	b.WriteString("Pusher does not hot reload. This is a measurement of what it would cost on\n")
	b.WriteString("this hub, so the decision to build it rests on numbers from the hardware\n")
	b.WriteString("rather than on somebody else's.\n\n")

	fmt.Fprintf(b, "| | |\n|---|---|\n")
	fmt.Fprintf(b, "| dex used | %s (the smallest non-stub dex in the APK) |\n", mb(reload.DexBytes))
	fmt.Fprintf(b, "| pushing it | %s |\n", secs(reload.Push))

	if reload.CompileOK {
		fmt.Fprintf(b, "| dex2oat on the hub | %s |\n", secs(reload.Compile))
		if reload.Overhead > 0 {
			fmt.Fprintf(b, "| of which startup | %s (measured on a %s stub) |\n",
				secs(reload.Overhead), mb(reload.StubBytes))
			fmt.Fprintf(b, "| of which real work | %s |\n", secs(reload.Marginal()))
		}
		fmt.Fprintf(b, "| **floor for a reload** | **%s** |\n\n", secs(reload.Floor()))

		if reload.Overhead > 0 && reload.Marginal() < reload.Overhead {
			b.WriteString("Most of that compile is dex2oat starting up, not compiling. A reload of\n")
			b.WriteString("code this size is dominated by fixed cost, which is the good case: it\n")
			b.WriteString("means the number barely grows with the amount of code changed.\n\n")
		}
	} else {
		fmt.Fprintf(b, "| dex2oat on the hub | not measured: %s |\n", reload.CompileWhy)
		fmt.Fprintf(b, "| floor for a reload | at least %s |\n\n", secs(reload.Push))
	}

	best := time.Duration(0)
	for _, run := range runs {
		if run.Err != nil || run.Skipped || strings.HasPrefix(run.Name, "Android Studio") {
			continue
		}
		if best == 0 || run.Total() < best {
			best = run.Total()
		}
	}

	if best > 0 && reload.Floor() > 0 && len(runs) > 0 {
		fmt.Fprintf(b, "Against the fastest real deploy measured above (%s), a reload would have to\n", secs(best))
		fmt.Fprintf(b, "beat %s to be worth the complexity.\n\n", secs(reload.Floor()))
	}

	b.WriteString("What this does **not** prove: that the classes would load. The FTC SDK finds\n")
	b.WriteString("OpModes by walking the base APK, then the split APKs, then whatever OnBotJava\n")
	b.WriteString("and the external-libraries mechanism report. Anything loaded outside those\n")
	b.WriteString("paths is invisible to the Driver Station no matter how fast it arrived.\n\n")
}

func writeSloth(b *strings.Builder, runs []Run) {
	b.WriteString("## Against Sloth\n\n")
	b.WriteString("**Pusher is not a Sloth replacement.** Sloth hot reloads: it sends only the\n")
	b.WriteString("team's code and loads it into a running app. Pusher installs an APK faster.\n")
	b.WriteString("Those are different problems, and below a few seconds they are not comparable.\n\n")

	b.WriteString("Their published figures, quoted from their README rather than measured here:\n\n")
	fmt.Fprintf(b, "| | claimed |\n|---|---|\n")
	fmt.Fprintf(b, "| Sloth, typical | %s |\n", SlothTypical)
	fmt.Fprintf(b, "| Sloth, worst case | %s |\n", SlothCeiling)
	fmt.Fprintf(b, "| FastLoad | %s |\n", FastLoadTime)
	fmt.Fprintf(b, "| a standard TeamCode upload | %s |\n\n", SlothVsFull)

	best := time.Duration(0)
	bestName := ""
	for _, run := range runs {
		if run.Err != nil || run.Skipped {
			continue
		}
		if best == 0 || run.Total() < best {
			best, bestName = run.Total(), run.Name
		}
	}

	if best > 0 {
		fmt.Fprintf(b, "Fastest real deploy measured here: **%s** (%s).\n\n", secs(best), bestName)
	} else {
		b.WriteString("No deploy was measured in this run, so there is nothing to compare.\n\n")
	}

	b.WriteString("Read that as a floor, not a headline. Everything pusher does still ends in a\n")
	b.WriteString("package manager install, and no amount of transfer cleverness removes dexopt.\n")
	b.WriteString("Getting into Sloth's range means not installing at all.\n")
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func mb(bytes int64) string {
	if bytes <= 0 {
		return "0"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d bytes", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.0f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

func secs(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// Summary is the short version for the menu.
func Summary(runs []Run) string {
	type row struct {
		name string
		d    time.Duration
	}

	var rows []row
	for _, run := range runs {
		if run.Err == nil {
			rows = append(rows, row{run.Name, run.Total()})
		}
	}

	sort.Slice(rows, func(a, b int) bool { return rows[a].d < rows[b].d })

	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-32s %s\n", r.name, secs(r.d))
	}
	return b.String()
}
