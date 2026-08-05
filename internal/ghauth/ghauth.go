// Package ghauth holds the GitHub token that proves access to the private blob
// repository, and answers whether this machine currently has that access.
//
// The token lives in its own file rather than config.yaml. A GitHub PAT reaches
// far beyond one robot, so it does not belong next to Wi-Fi passwords in a file
// people paste into issues.
package ghauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Repo is the private repository access is checked against.
	Repo = "PzmuV1517/blob"

	repoAPI = "https://api.github.com/repos/" + Repo
	userAPI = "https://api.github.com/user"

	// TTL is how long a successful check stands before GitHub is asked again.
	// Long enough that authenticating at home carries through a competition
	// weekend with no network.
	TTL = 7 * 24 * time.Hour
)

// Credentials is what gets stored on disk.
type Credentials struct {
	// Token is set only when it was typed in. A token found somewhere this
	// machine already keeps one is referenced by Source instead and read back
	// on demand, so pusher does not become a second place a GitHub token lives.
	Token  string `json:"token,omitempty"`
	Source string `json:"source,omitempty"`
	// Login and CheckedAt record the last check that succeeded, so an offline
	// machine can still be trusted.
	Login     string `json:"login,omitempty"`
	CheckedAt int64  `json:"checked_at,omitempty"`
	// Declined records that the token was removed on purpose. Without it, the
	// next lookup would find the machine's own GitHub login and undo that.
	Declined bool `json:"declined,omitempty"`
}

// Secret is the token itself, read from its source when it was not stored.
func (c Credentials) Secret() string {
	if c.Token != "" {
		return c.Token
	}
	if source, ok := sourceByID(c.Source); ok {
		return strings.TrimSpace(source.Read())
	}
	return ""
}

// Discovered reports whether the token came from elsewhere on this machine.
func (c Credentials) Discovered() bool { return c.Token == "" && c.Source != "" }

func (c Credentials) verified() bool { return c.CheckedAt > 0 }

func (c Credentials) fresh() bool {
	return c.verified() && time.Since(time.Unix(c.CheckedAt, 0)) < TTL
}

// Status is the outcome of resolving access.
type Status int

const (
	// NoToken is the only state that fails closed without asking anything.
	NoToken Status = iota
	// Denied means GitHub answered and this token cannot see the repo.
	Denied
	// Verified means GitHub was asked just now and said yes.
	Verified
	// Cached means a recent check said yes and has not expired.
	Cached
	// Offline means GitHub could not be reached but a previous check said yes.
	Offline
)

// OK reports whether the blob features should be available.
func (s Status) OK() bool {
	return s == Verified || s == Cached || s == Offline
}

func (s Status) String() string {
	switch s {
	case Denied:
		return "no access"
	case Verified:
		return "valid"
	case Cached:
		return "valid (cached)"
	case Offline:
		return "valid (offline)"
	}
	return "not set"
}

// Path is where credentials are kept.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "pusher", "credentials"), nil
}

func Load() (Credentials, error) {
	path, err := Path()
	if err != nil {
		return Credentials{}, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("cannot read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		// A corrupt file should not lock anyone out permanently.
		return Credentials{}, nil
	}
	return creds, nil
}

// Save writes credentials at 0600, and repairs the mode if the file already
// existed with something looser.
func Save(creds Credentials) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create the config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("cannot write credentials: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// Clear forgets the token and records that this was deliberate, so the next
// lookup does not immediately re-adopt the machine's own GitHub login.
func Clear() error {
	return Save(Credentials{Declined: true})
}

// SetToken validates a token and stores it only if GitHub accepts it, so a
// typo never displaces working credentials.
func SetToken(token string) (Credentials, error) {
	creds := Credentials{Token: token}

	login, err := validate(token)
	if err != nil {
		return creds, err
	}

	creds.Login = login
	creds.CheckedAt = time.Now().Unix()
	return creds, Save(creds)
}

// Resolve reports whether this machine has access.
//
// It fails closed only when there is no token. Once a token has checked out
// against GitHub, a later network failure leaves it trusted: competition venues
// routinely have no usable network, and locking the library out there would be
// worse than trusting a week-old answer.
func Resolve() (Status, Credentials) {
	creds, err := Load()
	if err != nil {
		return NoToken, creds
	}

	// Nothing to go on. Before asking anyone to paste a token, look for one
	// this machine already has: the people who can see the repository are
	// generally already signed in to GitHub somewhere.
	if creds.Secret() == "" {
		if creds.Declined {
			return NoToken, creds
		}
		if found, ok := discover(); ok {
			Save(found)
			return Verified, found
		}
		return NoToken, creds
	}

	if creds.fresh() {
		return Cached, creds
	}

	login, err := validate(creds.Secret())
	if err != nil {
		if isDenied(err) {
			// GitHub answered and said no. Drop the stale approval so the menu
			// stops claiming access it does not have.
			creds.Login, creds.CheckedAt = "", 0
			Save(creds)
			return Denied, creds
		}
		if creds.verified() {
			return Offline, creds
		}
		return NoToken, creds
	}

	creds.Login = login
	creds.CheckedAt = time.Now().Unix()
	Save(creds)
	return Verified, creds
}

// deniedError marks the case where GitHub answered and refused, as opposed to
// not being reachable. The two have to lead to different outcomes.
type deniedError struct{ msg string }

func (e *deniedError) Error() string { return e.msg }

func isDenied(err error) bool {
	_, ok := err.(*deniedError)
	return ok
}

// Client is an authenticated GitHub client.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// Request builds an authenticated GitHub request.
func Request(method, url, token string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

// validate confirms the token can see the blob repository and returns the login
// it belongs to.
func validate(token string) (string, error) {
	if token == "" {
		return "", &deniedError{"no token"}
	}

	client := Client(10 * time.Second)

	req, err := Request(http.MethodGet, repoAPI, token)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// GitHub returns 404, not 403, for a private repository the caller
		// cannot see. It does not mean the repository is gone.
		return "", &deniedError{"this token cannot see " + Repo + ".\n" +
			"Classic tokens need the repo scope; fine-grained tokens need Contents: Read on that repository"}
	case http.StatusUnauthorized:
		return "", &deniedError{"GitHub rejected the token"}
	default:
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}

	return login(client, token), nil
}

// login is cosmetic, so a failure here must not fail the check.
func login(client *http.Client, token string) string {
	req, err := Request(http.MethodGet, userAPI, token)
	if err != nil {
		return ""
	}

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var payload struct {
		Login string `json:"login"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return ""
	}
	return payload.Login
}
