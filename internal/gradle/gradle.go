package gradle

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/andreibanu/pusher/internal/config"
)

// wrapperName is gradlew everywhere except Windows, which needs the batch file.
func wrapperName() string {
	if runtime.GOOS == "windows" {
		return "gradlew.bat"
	}
	return "gradlew"
}

func DetectWrapper() (string, error) {
	name := wrapperName()

	wrapper := filepath.Join(".", name)
	if _, err := os.Stat(wrapper); err == nil {
		return wrapper, nil
	}

	for i := 0; i < 3; i++ {
		wrapper = filepath.Join(strings.Repeat("../", i+1), name)
		if _, err := os.Stat(wrapper); err == nil {
			absPath, _ := filepath.Abs(wrapper)
			return absPath, nil
		}
	}

	return "", fmt.Errorf("%s not found in current directory or parent directories", name)
}

// androidStudioJDK finds the JDK bundled with Android Studio, so a machine
// that has only ever built through the IDE still works without JAVA_HOME set.
func androidStudioJDK() string {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Android Studio.app/Contents/jbr/Contents/Home",
			filepath.Join(os.Getenv("HOME"), "Applications/Android Studio.app/Contents/jbr/Contents/Home"),
		}
	case "linux":
		home := os.Getenv("HOME")
		candidates = []string{
			"/opt/android-studio/jbr",
			"/usr/local/android-studio/jbr",
			filepath.Join(home, "android-studio/jbr"),
			// The JetBrains Toolbox and snap layouts.
			filepath.Join(home, ".local/share/JetBrains/Toolbox/apps/android-studio/jbr"),
			"/snap/android-studio/current/android-studio/jbr",
		}
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			if base != "" {
				candidates = append(candidates,
					filepath.Join(base, "Android", "Android Studio", "jbr"))
			}
		}
	}

	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}

	return ""
}

func ProjectDir(wrapper string) string {
	dir := filepath.Dir(wrapper)
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func Build(wrapper string, offline bool, outputWriter io.Writer) error {
	if _, err := os.Stat(wrapper); err != nil {
		return fmt.Errorf("gradle wrapper not found: %s", wrapper)
	}

	// Windows has no execute bit, and chmod there would be a no-op at best.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(wrapper, 0755); err != nil {
			return fmt.Errorf("failed to make %s executable: %w", wrapperName(), err)
		}
	}

	threads := config.GetThreads()
	args := []string{
		"assembleDebug",
		"--parallel",
		"--build-cache",
		fmt.Sprintf("-Dorg.gradle.workers.max=%d", threads),
	}
	if offline {
		args = append(args, "--offline")
	}

	cmd := exec.Command(wrapper, args...)

	wrapperDir := filepath.Dir(wrapper)
	cmd.Dir = wrapperDir

	if os.Getenv("JAVA_HOME") == "" {
		if jdk := androidStudioJDK(); jdk != "" {
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "JAVA_HOME="+jdk)
			cmd.Env = append(cmd.Env, "PATH="+filepath.Join(jdk, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gradle: %w", err)
	}

	done := make(chan bool)
	go streamOutput(stdout, outputWriter, done)
	go streamOutput(stderr, outputWriter, done)

	<-done
	<-done

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("gradle build failed: %w", err)
	}

	return nil
}

func streamOutput(reader io.Reader, writer io.Writer, done chan bool) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Fprintln(writer, scanner.Text())
	}
	done <- true
}

func FindApk(wrapperDir string) (string, error) {

	patterns := []string{
		filepath.Join(wrapperDir, "TeamCode", "build", "outputs", "apk", "debug", "*.apk"),
		filepath.Join(wrapperDir, "TeamCode", "build", "outputs", "apk", "debug", "TeamCode-debug.apk"),
		filepath.Join(wrapperDir, "FtcRobotController", "build", "outputs", "apk", "debug", "*.apk"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {

			return matches[0], nil
		}
	}

	return "", fmt.Errorf("debug APK not found in build outputs")
}
