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

	restore := discover
	discover = func() (Credentials, bool) { return Credentials{}, false }
	t.Cleanup(func() { discover = restore })

	return home
}

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

func TestCredentialsLiveOutsideTheConfigFile(t *testing.T) {
	home := isolate(t)

	if err := Save(Credentials{Token: "ghp_example"}); err != nil {
		t.Fatal(err)
	}

	path, _ := Path()
	if filepath.Base(path) != "credentials" {
		t.Errorf("stored at %s", path)
	}

	config := filepath.Join(home, ".config", "epsh", "config.yaml")
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

func TestResolveWithoutATokenFailsClosed(t *testing.T) {
	isolate(t)

	if status, _ := Resolve(); status != NoToken || status.OK() {
		t.Errorf("got %v, want a closed NoToken", status)
	}
}

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
