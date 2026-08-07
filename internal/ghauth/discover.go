package ghauth

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Source is somewhere this machine may already keep a GitHub token.
type Source struct {
	ID    string
	Label string
	Read  func() string
}

// Sources are the places a token is looked for, in order.
func Sources() []Source {
	return []Source{
		{"env", "GH_TOKEN in the environment", readEnv},
		{"gh", "the gh CLI", readGH},
		{"git", "git's credential helper", readGitCredential},
	}
}

func sourceByID(id string) (Source, bool) {
	for _, s := range Sources() {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// SourceLabel names where a stored credential reads its token from.
func SourceLabel(id string) string {
	if s, ok := sourceByID(id); ok {
		return s.Label
	}
	return ""
}

var discover = Discover

// Discover returns the first token on this machine that can see the repository.
func Discover() (Credentials, bool) {
	seen := map[string]bool{}

	for _, source := range Sources() {
		token := strings.TrimSpace(source.Read())
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true

		login, err := validate(token)
		if err != nil {
			continue
		}

		return Credentials{
			Source:    source.ID,
			Login:     login,
			CheckedAt: time.Now().Unix(),
		}, true
	}

	return Credentials{}, false
}

func readEnv() string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := os.Getenv(name); token != "" {
			return token
		}
	}
	return ""
}

func readGH() string {
	out, err := run("gh", "auth", "token")
	if err != nil {
		return ""
	}
	return out
}

func readGitCredential() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return credentialField(string(out), "password")
}

func credentialField(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), key+"="); ok {
			return value
		}
	}
	return ""
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
