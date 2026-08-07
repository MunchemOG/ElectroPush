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

// The private repository access is checked against, and how long a check stands.
const (
	Repo = "PzmuV1517/blob"

	repoAPI = "https://api.github.com/repos/" + Repo
	userAPI = "https://api.github.com/user"

	TTL = 7 * 24 * time.Hour
)

// Credentials is what gets stored on disk.
type Credentials struct {
	Token  string `json:"token,omitempty"`
	Source string `json:"source,omitempty"`

	Login     string `json:"login,omitempty"`
	CheckedAt int64  `json:"checked_at,omitempty"`

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

// The outcomes of resolving access. Only NoToken fails closed without asking.
const (
	NoToken Status = iota

	Denied

	Verified

	Cached

	Offline
)

// OK reports whether the blob features should be available.
func (s Status) OK() bool {
	return s == Verified || s == Cached || s == Offline
}

// String names the status for showing a person.
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

// Load reads the stored credentials.
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

		return Credentials{}, nil
	}
	return creds, nil
}

// Save writes credentials at 0600.
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

// Clear forgets the token and records that this was deliberate.
func Clear() error {
	return Save(Credentials{Declined: true})
}

// SetToken validates a token and stores it only if GitHub accepts it.
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

// Resolve reports whether this machine has access, failing closed only with no token.
func Resolve() (Status, Credentials) {
	creds, err := Load()
	if err != nil {
		return NoToken, creds
	}

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

type deniedError struct{ msg string }

// Error explains why GitHub refused.
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

		return "", &deniedError{"this token cannot see " + Repo + ".\n" +
			"Classic tokens need the repo scope; fine-grained tokens need Contents: Read on that repository"}
	case http.StatusUnauthorized:
		return "", &deniedError{"GitHub rejected the token"}
	default:
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}

	return login(client, token), nil
}

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
