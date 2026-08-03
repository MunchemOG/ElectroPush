package gradle

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/andreibanu/pusher/internal/config"
)

func DetectWrapper() (string, error) {

	wrapper := "./gradlew"
	if _, err := os.Stat(wrapper); err == nil {
		return wrapper, nil
	}

	for i := 0; i < 3; i++ {
		wrapper = filepath.Join(strings.Repeat("../", i+1), "gradlew")
		if _, err := os.Stat(wrapper); err == nil {
			absPath, _ := filepath.Abs(wrapper)
			return absPath, nil
		}
	}

	return "", fmt.Errorf("gradlew not found in current directory or parent directories")
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

	if err := os.Chmod(wrapper, 0755); err != nil {
		return fmt.Errorf("failed to make gradlew executable: %w", err)
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
		candidate := "/Applications/Android Studio.app/Contents/jbr/Contents/Home"
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			if cmd.Env == nil {
				cmd.Env = os.Environ()
			}
			cmd.Env = append(cmd.Env, "JAVA_HOME="+candidate)
			cmd.Env = append(cmd.Env, "PATH="+filepath.Join(candidate, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
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
