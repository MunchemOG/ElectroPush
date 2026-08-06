package extreme

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/andreibanu/pusher/internal/gradle"
	"github.com/andreibanu/pusher/internal/hotreload"
)

// Module is the gradle module holding the team's own code.
const Module = "TeamCode"

// SourceRoot is where that module keeps it.
var SourceRoot = filepath.Join("TeamCode", "src", "main", "java")

// Project is a team's FTC project seen as something that can be reloaded.
type Project struct {
	Root    string
	Wrapper string
}

// Build is what a compile produced.
type Build struct {
	Jar     string
	Dex     string
	Sources int
	Classes int
}

// FindProject locates the FTC project around the working directory.
func FindProject() (*Project, error) {
	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return nil, fmt.Errorf("not inside an FTC project: %w", err)
	}

	root := gradle.ProjectDir(wrapper)
	if _, err := os.Stat(filepath.Join(root, SourceRoot)); err != nil {
		return nil, fmt.Errorf("no %s in %s", SourceRoot, root)
	}

	return &Project{Root: root, Wrapper: wrapper}, nil
}

// Sources lists the team's Java files.
func (p *Project) Sources() ([]string, error) {
	root := filepath.Join(p.Root, SourceRoot)

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".java") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no Java sources under %s", root)
	}
	return files, nil
}

// Compile turns the team's sources into a jar and a dex.
//
// Everything is compiled, not only what changed. A reload replaces the whole
// classloader, so a partial dex would leave the classes it did not contain
// unresolvable, and the SDK's re-registration abandons everything on the first
// failure rather than skipping one class.
func Compile(p *Project, cp Classpath, work string) (Build, error) {
	var out Build

	tc, err := hotreload.FindToolchain()
	if err != nil {
		return out, err
	}
	defer tc.Cleanup()

	sources, err := p.Sources()
	if err != nil {
		return out, err
	}
	out.Sources = len(sources)

	classes := filepath.Join(work, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return out, err
	}

	// The file list goes in an argument file. A hundred and twenty paths
	// exceeds what a command line takes on some platforms, and javac has
	// supported @files for exactly this since forever.
	list := filepath.Join(work, "sources.txt")
	if err := os.WriteFile(list, []byte(strings.Join(sources, "\n")), 0o644); err != nil {
		return out, err
	}

	// Java 8, matching what the FTC project targets and what the hub runs. A
	// modern javac also refuses -bootclasspath unless the target is old enough
	// to need one, which is exactly the case here.
	args := append([]string{
		"-source", "8", "-target", "8",
		"-nowarn",
		"-encoding", "UTF-8",
		"-d", classes,
	}, cp.Args()...)
	args = append(args, "@"+list)

	javac := exec.Command(tc.Javac, args...)
	if result, err := javac.CombinedOutput(); err != nil {
		return out, fmt.Errorf("compiling %s failed:\n%s", Module, lastLines(string(result), 25))
	}

	compiled, err := classFiles(classes)
	if err != nil {
		return out, err
	}
	out.Classes = len(compiled)

	out.Jar = filepath.Join(work, "teamcode.jar")
	if err := writeJar(out.Jar, classes, compiled); err != nil {
		return out, err
	}

	dexDir := filepath.Join(work, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return out, err
	}

	// d8 takes the jar rather than the loose classes: one argument instead of
	// hundreds, and it is the same input the robot will be given.
	d8 := exec.Command(tc.D8, "--min-api", "24", "--output", dexDir, out.Jar)
	if result, err := d8.CombinedOutput(); err != nil {
		return out, fmt.Errorf("dexing %s failed:\n%s", Module, lastLines(string(result), 25))
	}

	out.Dex = filepath.Join(dexDir, "classes.dex")
	if _, err := os.Stat(out.Dex); err != nil {
		return out, fmt.Errorf("d8 produced no classes.dex")
	}

	return out, nil
}

// classFiles lists what javac produced, relative to the output directory.
func classFiles(root string) ([]string, error) {
	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".class") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("javac produced no classes")
	}
	return files, nil
}

// writeJar packs the compiled classes under their package paths, which is where
// the SDK reads class names from.
func writeJar(jarPath, root string, entries []string) error {
	out, err := os.Create(jarPath)
	if err != nil {
		return err
	}
	defer out.Close()

	archive := zip.NewWriter(out)

	for _, entry := range entries {
		source, err := os.Open(filepath.Join(root, filepath.FromSlash(entry)))
		if err != nil {
			return err
		}

		w, err := archive.Create(entry)
		if err != nil {
			source.Close()
			return err
		}
		if _, err := io.Copy(w, source); err != nil {
			source.Close()
			return err
		}
		source.Close()
	}

	return archive.Close()
}

// gradleEnv gives Gradle a JDK when the environment has not.
func gradleEnv() []string {
	env := os.Environ()
	if os.Getenv("JAVA_HOME") != "" {
		return env
	}

	tc, err := hotreload.FindToolchain()
	if err != nil {
		return env
	}
	defer tc.Cleanup()

	// javac lives in <home>/bin, so the home is two levels up.
	home := filepath.Dir(filepath.Dir(tc.Javac))
	return append(env, "JAVA_HOME="+home)
}

// classNames lists the classes in a jar, the same way the SDK does.
func classNames(jarPath string) ([]string, error) {
	archive, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	var names []string
	for _, entry := range archive.File {
		if strings.HasSuffix(entry.Name, ".class") {
			names = append(names, strings.TrimSuffix(entry.Name, ".class"))
		}
	}
	return names, nil
}
