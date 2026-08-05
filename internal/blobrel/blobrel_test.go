package blobrel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aar is the smallest thing that passes the zip sniff.
var aar = append([]byte{'P', 'K', 3, 4}, []byte("payload")...)

func TestAssetNameMatchesTheAgreedContract(t *testing.T) {
	if got := AssetName(Competition, "v1.4.0"); got != "blob-competition-v1.4.0.aar" {
		t.Errorf("got %q", got)
	}
	if got := AssetName(Dev, "v1.4.0"); got != "blob-dev-v1.4.0.aar" {
		t.Errorf("got %q", got)
	}
}

// The asset endpoint redirects to object storage, which rejects the request if
// GitHub's Authorization header is still attached. Go forwards headers across
// redirects by default, so this is the one that silently breaks downloads.
func TestFetchDoesNotForwardTheTokenAcrossTheRedirect(t *testing.T) {
	var storageSawAuth bool

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			storageSawAuth = true
			http.Error(w, "credentials not supported", http.StatusBadRequest)
			return
		}
		w.Write(aar)
	}))
	defer storage.Close()

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/tags/v1.4.0"):
			fmt.Fprintf(w, `{"assets":[{"id":7,"name":"blob-dev-v1.4.0.aar","size":11}]}`)
		case strings.HasSuffix(r.URL.Path, "/releases/assets/7"):
			http.Redirect(w, r, storage.URL+"/blob.aar", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()

	api = gh.URL
	defer func() { api = "https://api.github.com/repos/PzmuV1517/blob" }()

	data, err := Fetch("ghp_example", Dev, "v1.4.0")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if storageSawAuth {
		t.Error("the token was forwarded to object storage")
	}
	if string(data) != string(aar) {
		t.Errorf("got %q", data)
	}
}

func TestFetchNamesWhatIsActuallyThere(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"assets":[{"id":1,"name":"blob-competition-v1.4.0.aar"}]}`)
	}))
	defer gh.Close()

	api = gh.URL
	defer func() { api = "https://api.github.com/repos/PzmuV1517/blob" }()

	_, err := Fetch("ghp_example", Dev, "v1.4.0")
	if err == nil {
		t.Fatal("expected a failure when the variant is not attached")
	}
	if !strings.Contains(err.Error(), "blob-competition-v1.4.0.aar") {
		t.Errorf("the error should list what is available, got: %v", err)
	}
}

// A 404 on a private repo means the token cannot see it, and the message has to
// say that rather than implying the release is gone.
func TestNotFoundBlamesAccessNotExistence(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer gh.Close()

	api = gh.URL
	defer func() { api = "https://api.github.com/repos/PzmuV1517/blob" }()

	_, err := LatestTag("ghp_example")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "cannot see") {
		t.Errorf("got %v", err)
	}
}

// An error page served with a 200 must not be written into the project as a
// library.
func TestFetchRejectsSomethingThatIsNotAnAAR(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>gateway timeout</html>"))
	}))
	defer storage.Close()

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/tags/v1.4.0"):
			fmt.Fprintf(w, `{"assets":[{"id":7,"name":"blob-dev-v1.4.0.aar"}]}`)
		default:
			http.Redirect(w, r, storage.URL+"/blob.aar", http.StatusFound)
		}
	}))
	defer gh.Close()

	api = gh.URL
	defer func() { api = "https://api.github.com/repos/PzmuV1517/blob" }()

	if _, err := Fetch("ghp_example", Dev, "v1.4.0"); err == nil {
		t.Error("HTML served as an AAR should be rejected")
	}
}

func TestIsZip(t *testing.T) {
	if !isZip(aar) {
		t.Error("a zip header should be recognised")
	}
	for _, bad := range [][]byte{nil, []byte("PK"), []byte("<html>")} {
		if isZip(bad) {
			t.Errorf("%q is not a zip", bad)
		}
	}
}
