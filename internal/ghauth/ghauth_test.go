package ghauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// Tests must not adopt whatever GitHub login the machine running them is
	// signed in to, which would make the result depend on the developer.
	restore := discover
	discover = func() (Credentials, bool) { return Credentials{}, false }
	t.Cleanup(func() { discover = restore })

	return home
}

// findable makes discovery succeed, standing in for a machine that is already
// signed in to an account with access.
func findable(t *testing.T, creds Credentials) {
	t.Helper()
	restore := discover
	discover = func() (Credentials, bool) { return creds, true }
	t.Cleanup(func() { discover = restore })
}

func TestSaveWritesOwnerOnly(t *testing.T) {
	isolate(t)

	if err := Save(Credentials{Token: "ghp_example"}); err != nil {
		t.Fatal(err)
	}

	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials are %v, want 0600", perm)
	}
}

// A token must not end up in config.yaml, which people paste into issues.
func TestCredentialsLiveOutsideTheConfigFile(t *testing.T) {
	home := isolate(t)

	if err := Save(Credentials{Token: "ghp_example"}); err != nil {
		t.Fatal(err)
	}

	path, _ := Path()
	if filepath.Base(path) != "credentials" {
		t.Errorf("stored at %s", path)
	}

	config := filepath.Join(home, ".config", "pusher", "config.yaml")
	if data, err := os.ReadFile(config); err == nil {
		if string(data) != "" && contains(string(data), "ghp_example") {
			t.Error("the token leaked into config.yaml")
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && filepath.Base(haystack) != needle &&
		len(needle) > 0 && stringIndex(haystack, needle) >= 0
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestLoadTreatsAMissingFileAsNoToken(t *testing.T) {
	isolate(t)

	creds, err := Load()
	if err != nil {
		t.Fatalf("a missing file is not an error: %v", err)
	}
	if creds.Token != "" {
		t.Errorf("got %+v", creds)
	}
}

// A corrupt file must not lock someone out with no way back.
func TestLoadSurvivesCorruption(t *testing.T) {
	isolate(t)

	path, _ := Path()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte("this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, err := Load()
	if err != nil {
		t.Errorf("corruption should read as no credentials, got %v", err)
	}
	if creds.Token != "" {
		t.Errorf("got %+v", creds)
	}
}

func TestClearRemovesTheToken(t *testing.T) {
	isolate(t)

	Save(Credentials{Token: "ghp_example"})
	if err := Clear(); err != nil {
		t.Fatal(err)
	}

	creds, _ := Load()
	if creds.Token != "" {
		t.Error("token survived Clear")
	}
	if err := Clear(); err != nil {
		t.Errorf("clearing twice is not an error: %v", err)
	}
}

// Fail closed only when there is no token at all. Resolve does no network here
// because a fresh cache short-circuits it, which is the whole point.
func TestResolveWithoutATokenFailsClosed(t *testing.T) {
	isolate(t)

	if status, _ := Resolve(); status != NoToken || status.OK() {
		t.Errorf("got %v, want a closed NoToken", status)
	}
}

// The point of discovery: someone already signed in is never asked to paste a
// token, and what gets stored references the source rather than copying it.
func TestResolveAdoptsAnExistingGitHubLogin(t *testing.T) {
	isolate(t)
	findable(t, Credentials{Source: "env", Login: "someone", CheckedAt: time.Now().Unix()})

	status, creds := Resolve()
	if status != Verified || !status.OK() {
		t.Fatalf("got %v, want Verified", status)
	}
	if creds.Login != "someone" || !creds.Discovered() {
		t.Errorf("got %+v", creds)
	}

	// And it is remembered, so the probes do not run on every call.
	stored, _ := Load()
	if stored.Source != "env" {
		t.Errorf("discovery was not persisted: %+v", stored)
	}
	if stored.Token != "" {
		t.Error("a discovered token must not be copied into the file")
	}
}

func TestResolveTrustsAFreshCacheWithoutNetwork(t *testing.T) {
	isolate(t)

	Save(Credentials{
		Token:     "ghp_example",
		Login:     "someone",
		CheckedAt: time.Now().Add(-time.Hour).Unix(),
	})

	status, creds := Resolve()
	if status != Cached || !status.OK() {
		t.Errorf("got %v, want Cached", status)
	}
	if creds.Login != "someone" {
		t.Errorf("login lost: %+v", creds)
	}
}

// The cache has to outlast a competition weekend spent offline.
func TestCacheLastsLongEnoughForAnEvent(t *testing.T) {
	if TTL < 3*24*time.Hour {
		t.Errorf("TTL of %v is too short to cover an event away from network", TTL)
	}
}

func TestFreshnessBoundary(t *testing.T) {
	cases := []struct {
		age   time.Duration
		fresh bool
	}{
		{time.Minute, true},
		{TTL - time.Hour, true},
		{TTL + time.Hour, false},
	}

	for _, c := range cases {
		creds := Credentials{Token: "x", CheckedAt: time.Now().Add(-c.age).Unix()}
		if got := creds.fresh(); got != c.fresh {
			t.Errorf("age %v: fresh=%v, want %v", c.age, got, c.fresh)
		}
	}
}

// A token that has never checked out is not trusted just because it exists.
func TestUnverifiedCredentialsAreNotFresh(t *testing.T) {
	creds := Credentials{Token: "x"}
	if creds.verified() || creds.fresh() {
		t.Error("a token with no successful check must not count as verified")
	}
}

func TestStatusOK(t *testing.T) {
	ok := map[Status]bool{
		NoToken:  false,
		Denied:   false,
		Verified: true,
		Cached:   true,
		Offline:  true,
	}
	for status, want := range ok {
		if got := status.OK(); got != want {
			t.Errorf("%v.OK() = %v, want %v", status, got, want)
		}
	}
}

// A 404 from GitHub means "you cannot see this", not "it does not exist", and
// has to fail closed rather than being mistaken for a network problem.
func TestDeniedIsDistinguishableFromUnreachable(t *testing.T) {
	denied := &deniedError{"no access"}
	if !isDenied(denied) {
		t.Error("a denial should be recognised as one")
	}
	if isDenied(os.ErrNotExist) {
		t.Error("an ordinary error must not read as a denial")
	}
}

func TestRequestCarriesTheToken(t *testing.T) {
	req, err := Request("GET", "https://api.github.com/user", "ghp_example")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ghp_example" {
		t.Errorf("got %q", got)
	}
	if req.Header.Get("Accept") == "" {
		t.Error("the GitHub Accept header is missing")
	}
}
