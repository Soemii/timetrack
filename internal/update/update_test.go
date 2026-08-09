package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.9.9", "v2.0.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"1.0.0", "v1.0.1", true}, // ohne v-Präfix
		{"v1.0.0", "v1.0.10", true},
		{"dev", "v1.0.0", false}, // unparsebar ⇒ nicht neuer
		{"v1.0.0", "banana", false},
		{"v1.0", "v1.0.1", false}, // zu wenig Teile
		{"v1.3.0-beta.1", "v1.3.0", true}, // Release schlägt Prerelease
		{"v1.3.0", "v1.3.0-beta.1", false},
		{"v1.3.0-beta.1", "v1.2.9", false}, // Stable-Kanal wartet, bis Stable überholt
		{"v1.2.9", "v1.3.0-beta.1", true},
		{"v1.3.0-beta.1", "v1.3.0-beta.2", true},
		{"v1.3.0-beta.9", "v1.3.0-beta.10", true}, // numerisch, nicht lexikalisch
		{"v1.3.0-alpha", "v1.3.0-alpha.1", true},  // weniger Felder < mehr Felder
		{"v1.3.0-1", "v1.3.0-beta", true},         // numerisch < alphanumerisch
		{"v1.3.0-alpha.2", "v1.3.0-beta.1", true},
		{"v1.3.0-beta.1", "v1.3.0-beta.1", false},
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// startFakeRelease startet einen Server, der Release-JSON, Binary und
// Checksums liefert. sumLine erlaubt kaputte Checksummen zu testen.
func startFakeRelease(t *testing.T, binary []byte, sumLine string) *httptest.Server {
	t.Helper()
	// Isoliert vom echten ~/.timetrack/update-channel des Dev-Rechners.
	t.Setenv("HOME", t.TempDir())
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}]}`,
			assetName(), srv.URL+"/bin", srv.URL+"/sums")
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(binary)
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, sumLine)
	})
	t.Setenv("TIMETRACK_UPDATE_URL", srv.URL+"/release")
	return srv
}

func writeChannel(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".timetrack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-channel"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPrereleaseChannelFetch(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// v1.2.9 steht vorn (created-Datum), Beta ist aber die höchste Version.
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"v1.2.9","assets":[]},
			{"tag_name":"v1.3.0-beta.2","assets":[]},
			{"tag_name":"banana","assets":[]}]`)
	})
	t.Setenv("TIMETRACK_UPDATE_URL", srv.URL+"/releases")
	writeChannel(t, "prerelease\n")

	rel, err := fetchRelease(srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.3.0-beta.2" {
		t.Errorf("tag = %q, want v1.3.0-beta.2", rel.TagName)
	}
}

func TestGarbageChannelFileStaysStable(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9","assets":[]}`)
	})
	t.Setenv("TIMETRACK_UPDATE_URL", srv.URL+"/release")
	writeChannel(t, "banana")

	rel, err := fetchRelease(srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v9.9.9" {
		t.Errorf("tag = %q, want v9.9.9", rel.TagName)
	}
}

func overrideExecutable(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "timetrack")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := executable
	executable = func() (string, error) { return target, nil }
	t.Cleanup(func() { executable = orig })
	return target
}

func TestRunReplacesBinary(t *testing.T) {
	binary := []byte("new shiny binary")
	sum := sha256.Sum256(binary)
	startFakeRelease(t, binary, hex.EncodeToString(sum[:])+"  "+assetName())
	target := overrideExecutable(t)

	var out strings.Builder
	if err := Run("v1.0.0", &out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Errorf("target content = %q, want %q", got, binary)
	}
	fi, _ := os.Stat(target)
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
	}
	if !strings.Contains(out.String(), "v9.9.9") {
		t.Errorf("output missing new version: %q", out.String())
	}
}

func TestRunChecksumMismatch(t *testing.T) {
	startFakeRelease(t, []byte("new shiny binary"),
		strings.Repeat("0", 64)+"  "+assetName())
	target := overrideExecutable(t)

	err := Run("v1.0.0", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "Checksumme") {
		t.Fatalf("want checksum error, got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old binary" {
		t.Errorf("target was modified despite checksum mismatch: %q", got)
	}
}

func TestRunAlreadyCurrent(t *testing.T) {
	startFakeRelease(t, nil, "")
	var out strings.Builder
	if err := Run("v9.9.9", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "aktuell") {
		t.Errorf("output = %q, want 'aktuell'", out.String())
	}
}

func TestRunDevBuild(t *testing.T) {
	if err := Run("dev", io.Discard); err == nil {
		t.Fatal("want error for dev build")
	}
}
