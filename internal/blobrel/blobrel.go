// Package blobrel fetches blob library builds from the private repository's
// GitHub releases. JitPack cannot build a private repo, so pusher carries the
// AAR itself and the FTC project never needs credentials of its own.
package blobrel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/andreibanu/pusher/internal/ghauth"
)

// api is a variable so tests can point it at a stub. The redirect behaviour
// that has to be verified only happens against a real server.
var api = "https://api.github.com/repos/" + ghauth.Repo

// Variant is which build of the library to fetch.
type Variant string

const (
	// Competition carries no path-recording code at all.
	Competition Variant = "blob-competition"
	// Dev records traces for the visualiser.
	Dev Variant = "blob-dev"
)

// AssetName is what CI attaches to a release: blob-competition-v1.4.0.aar.
func AssetName(v Variant, tag string) string {
	return fmt.Sprintf("%s-%s.aar", v, tag)
}

// LatestTag returns the newest published release tag.
func LatestTag(token string) (string, error) {
	var payload struct {
		Tag string `json:"tag_name"`
	}
	if err := get(token, api+"/releases/latest", &payload); err != nil {
		return "", err
	}
	if payload.Tag == "" {
		return "", fmt.Errorf("blob has no published releases")
	}
	return payload.Tag, nil
}

// Tags lists published release tags, newest first.
func Tags(token string) ([]string, error) {
	var payload []struct {
		Tag string `json:"tag_name"`
	}
	if err := get(token, api+"/releases?per_page=30", &payload); err != nil {
		return nil, err
	}

	tags := make([]string, 0, len(payload))
	for _, r := range payload {
		if r.Tag != "" {
			tags = append(tags, r.Tag)
		}
	}
	return tags, nil
}

// asset is one file attached to a release.
type asset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Fetch downloads the AAR for a variant at a tag.
func Fetch(token string, v Variant, tag string) ([]byte, error) {
	var release struct {
		Assets []asset `json:"assets"`
	}
	if err := get(token, api+"/releases/tags/"+tag, &release); err != nil {
		return nil, err
	}

	want := AssetName(v, tag)
	for _, a := range release.Assets {
		if a.Name == want {
			return download(token, a.ID)
		}
	}

	return nil, fmt.Errorf("release %s has no %s\navailable: %s",
		tag, want, names(release.Assets))
}

func names(assets []asset) string {
	if len(assets) == 0 {
		return "nothing attached"
	}
	out := make([]string, 0, len(assets))
	for _, a := range assets {
		out = append(out, a.Name)
	}
	return strings.Join(out, ", ")
}

// download pulls one asset by id.
//
// The asset endpoint answers with a redirect to object storage, which rejects
// the request outright if GitHub's Authorization header is still attached. Go
// forwards headers across redirects by default, so it has to be stripped.
func download(token string, id int64) ([]byte, error) {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Authorization")
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	url := fmt.Sprintf("%s/releases/assets/%d", api, id)
	req, err := ghauth.Request(http.MethodGet, url, token)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot download the library: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download was cut short: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("download was empty")
	}
	if !isZip(data) {
		// An AAR is a zip. Anything else means an error page came back with a
		// 200, which is worth catching before it lands in the project.
		return nil, fmt.Errorf("what came back is not an AAR")
	}
	return data, nil
}

func isZip(data []byte) bool {
	return len(data) > 4 && data[0] == 'P' && data[1] == 'K' &&
		(data[2] == 3 || data[2] == 5 || data[2] == 7)
}

func get(token, url string, into any) error {
	req, err := ghauth.Request(http.MethodGet, url, token)
	if err != nil {
		return err
	}

	resp, err := ghauth.Client(15 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return fmt.Errorf("not found, or this token cannot see %s", ghauth.Repo)
	default:
		return fmt.Errorf("GitHub returned %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("cannot read GitHub response: %w", err)
	}
	return nil
}
