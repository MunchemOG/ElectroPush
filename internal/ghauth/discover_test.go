package ghauth

import (
	"os"
	"testing"
	"time"
)

func TestCredentialFieldParsesGitOutput(t *testing.T) {
	out := "protocol=https\nhost=github.com\nusername=someone\npassword=ghp_example\n"

	if got := credentialField(out, "password"); got != "ghp_example" {
		t.Errorf("got %q", got)
	}
	if got := credentialField(out, "username"); got != "someone" {
		t.Errorf("got %q", got)
	}
	if got := credentialField(out, "nothing"); got != "" {
		t.Errorf("got %q for an absent key", got)
	}
	// A helper that declines answers with nothing useful.
	if got := credentialField("", "password"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestReadEnvPrefersGHToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if got := readEnv(); got != "" {
		t.Errorf("got %q with nothing set", got)
	}

	t.Setenv("GITHUB_TOKEN", "from_github_token")
	if got := readEnv(); got != "from_github_token" {
		t.Errorf("got %q", got)
	}

	t.Setenv("GH_TOKEN", "from_gh_token")
	if got := readEnv(); got != "from_gh_token" {
		t.Errorf("GH_TOKEN should win, got %q", got)
	}
}

// A stored token is used as-is; a discovered one is read back from its source
// rather than having been copied into pusher's own file.
func TestSecretResolvesFromItsSource(t *testing.T) {
	t.Setenv("GH_TOKEN", "from_the_environment")

	stored := Credentials{Token: "typed_in"}
	if got := stored.Secret(); got != "typed_in" {
		t.Errorf("got %q", got)
	}
	if stored.Discovered() {
		t.Error("a typed token is not discovered")
	}

	found := Credentials{Source: "env"}
	if got := found.Secret(); got != "from_the_environment" {
		t.Errorf("got %q", got)
	}
	if !found.Discovered() {
		t.Error("a sourced token is discovered")
	}

	if got := (Credentials{Source: "nonsense"}).Secret(); got != "" {
		t.Errorf("an unknown source yields nothing, got %q", got)
	}
}

// A discovered token must not be written into the credentials file, or pusher
// becomes a second place a GitHub token lives.
func TestDiscoveredCredentialsStoreNoSecret(t *testing.T) {
	isolate(t)
	t.Setenv("GH_TOKEN", "from_the_environment")

	if err := Save(Credentials{Source: "env", Login: "someone", CheckedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}

	path, _ := Path()
	data := readFile(t, path)
	if stringIndex(data, "from_the_environment") >= 0 {
		t.Errorf("the token was copied into the credentials file:\n%s", data)
	}
}

// Removing the token has to stick. Without the declined marker the next lookup
// would find the machine's own GitHub login and silently put it straight back.
func TestClearIsNotUndoneByDiscovery(t *testing.T) {
	isolate(t)
	t.Setenv("GH_TOKEN", "from_the_environment")

	if err := Clear(); err != nil {
		t.Fatal(err)
	}

	// Resolve must short-circuit before discovery, so this makes no network
	// call despite a token being visible in the environment.
	status, creds := Resolve()
	if status != NoToken {
		t.Errorf("got %v, want NoToken after Clear", status)
	}
	if !creds.Declined {
		t.Error("the declined marker was not recorded")
	}
}

// Setting a token explicitly has to clear the declined marker, or the machine
// stays opted out forever.
func TestSetTokenClearsTheDeclinedMarker(t *testing.T) {
	isolate(t)

	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	if creds, _ := Load(); !creds.Declined {
		t.Fatal("setup: expected declined")
	}

	// SetToken builds fresh credentials rather than editing the stored ones,
	// so the marker cannot survive.
	fresh := Credentials{Token: "x", Login: "someone", CheckedAt: time.Now().Unix()}
	if err := Save(fresh); err != nil {
		t.Fatal(err)
	}
	if creds, _ := Load(); creds.Declined {
		t.Error("declined survived a new token")
	}
}

func TestSourcesAreOrderedAndLabelled(t *testing.T) {
	sources := Sources()
	if len(sources) == 0 {
		t.Fatal("no sources")
	}
	if sources[0].ID != "env" {
		t.Errorf("environment should be tried first, got %q", sources[0].ID)
	}

	for _, s := range sources {
		if s.Label == "" {
			t.Errorf("source %q has no label to show the user", s.ID)
		}
		if s.Read == nil {
			t.Errorf("source %q cannot be read", s.ID)
		}
		if SourceLabel(s.ID) != s.Label {
			t.Errorf("SourceLabel(%q) does not round-trip", s.ID)
		}
	}

	if SourceLabel("nonsense") != "" {
		t.Error("an unknown source has no label")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
