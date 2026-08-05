package ghauth

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// A Source is somewhere this machine may already keep a GitHub token. Most
// people who can see the private repository are signed in to GitHub already, so
// asking them to paste a token is usually asking for something they have.
type Source struct {
	// ID is recorded in the credentials file. The token itself is read back
	// from here on demand rather than copied, so pusher does not become a
	// second place a GitHub token lives.
	ID    string
	Label string
	Read  func() string
}

// Sources are tried in order. Environment first because it is explicit and
// costs nothing, then gh, then git's credential helper.
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

// discover is indirected so tests can run without adopting whatever GitHub
// login the machine running them happens to be signed in to.
var discover = Discover

// Discover looks for a token this machine already has and returns the first one
// that can actually see the repository.
//
// Only a token that resolves is accepted, so a machine signed in to an account
// without access falls through to being asked rather than being told no.
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

// readGH asks the gh CLI for its token. gh exits non-zero when it is not
// installed or not signed in, which is not worth reporting.
func readGH() string {
	out, err := run("gh", "auth", "token")
	if err != nil {
		return ""
	}
	return out
}

// readGitCredential asks git for the github.com credential it already stores.
//
// GIT_TERMINAL_PROMPT=0 matters: without it git will sit waiting for a username
// on a machine with no helper configured, which inside a full-screen menu looks
// like a hang.
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

// credentialField pulls one key out of git credential's key=value output.
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
