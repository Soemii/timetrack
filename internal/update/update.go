// Package update prüft GitHub-Releases und ersetzt das laufende Binary.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ponytail: one env knob doubles as the smoke-test hook
func apiURL() string {
	if u := os.Getenv("TIMETRACK_UPDATE_URL"); u != "" {
		return u
	}
	return "https://api.github.com/repos/Soemii/timetrack/releases/latest"
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// executable ist eine Variable, damit Tests das Ziel umbiegen können.
var executable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// assetName muss zum name_template in .goreleaser.yaml passen.
func assetName() string {
	name := "timetrack_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func fetchRelease(c *http.Client) (*release, error) {
	body, err := download(c, apiURL())
	if err != nil {
		return nil, err
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func download(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// versionLess meldet, ob a < b (Tags wie v1.2.3). Unparsebares gilt als nicht-neuer.
// ponytail: plain vX.Y.Z tags only, no pre-release ordering
func versionLess(a, b string) bool {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// MaybeNotify prüft höchstens einmal pro 24h auf eine neuere Version und
// schreibt dann einen Hinweis. Jeder Fehler ist still — der eigentliche
// Befehl darf am Update-Check nie scheitern.
func MaybeNotify(current string, out io.Writer) {
	if current == "dev" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".timetrack")
	stamp := filepath.Join(dir, "last-update-check")
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		return
	}
	// Stempel vor dem Netz-Call, damit ein Offline-Tag nur einmal 2s kostet.
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(stamp, nil, 0o644) != nil {
		return
	}
	rel, err := fetchRelease(&http.Client{Timeout: 2 * time.Second})
	if err != nil {
		return
	}
	if versionLess(current, rel.TagName) {
		fmt.Fprintf(out, "Neue Version %s verfügbar — 'timetrack update' installiert sie.\n", rel.TagName)
	}
}

// Run lädt das neueste Release-Binary, verifiziert die SHA256-Checksumme und
// ersetzt das laufende Binary.
func Run(current string, out io.Writer) error {
	if current == "dev" {
		return fmt.Errorf("dev-Build ohne Version — bitte ein Release-Binary von GitHub verwenden")
	}
	rel, err := fetchRelease(&http.Client{Timeout: 10 * time.Second})
	if err != nil {
		return err
	}
	if !versionLess(current, rel.TagName) {
		fmt.Fprintf(out, "Bereits aktuell (%s).\n", current)
		return nil
	}

	name := assetName()
	var binURL, sumURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case name:
			binURL = a.URL
		case "checksums.txt":
			sumURL = a.URL
		}
	}
	if binURL == "" || sumURL == "" {
		return fmt.Errorf("Release %s: Asset %q oder checksums.txt fehlt", rel.TagName, name)
	}

	fmt.Fprintf(out, "Lade %s herunter …\n", rel.TagName)
	c := &http.Client{Timeout: 5 * time.Minute}
	data, err := download(c, binURL)
	if err != nil {
		return err
	}
	sums, err := download(c, sumURL)
	if err != nil {
		return err
	}
	if err := verifyChecksum(data, sums, name); err != nil {
		return err
	}

	target, err := executable()
	if err != nil {
		return err
	}
	if err := replaceExecutable(target, data); err != nil {
		return err
	}
	fmt.Fprintf(out, "Aktualisiert auf %s.\n", rel.TagName)
	return nil
}

func verifyChecksum(data, sums []byte, name string) error {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("keine Checksumme für %s in checksums.txt", name)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("Checksumme stimmt nicht (erwartet %s, erhalten %s) — Download verworfen", want, got)
	}
	return nil
}

func replaceExecutable(target string, data []byte) error {
	// Gleiches Verzeichnis wie das Ziel: gleiches Dateisystem, Rename ist atomar.
	tmp, err := os.CreateTemp(filepath.Dir(target), ".timetrack-new-*")
	if err != nil {
		return fmt.Errorf("%w — fehlen Schreibrechte im Installationsverzeichnis?", err)
	}
	defer os.Remove(tmp.Name()) // nach erfolgreichem Rename ein No-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		// Windows: laufende exe darf umbenannt, aber nicht überschrieben werden.
		old := target + ".old"
		os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), target); err != nil {
			os.Rename(old, target) // best effort Rollback
			return err
		}
		return nil
	}
	return os.Rename(tmp.Name(), target)
}

// CleanupOld entfernt das nach einem Windows-Update zurückgelassene .old-Binary.
// Fehler egal — solange der alte Prozess läuft, klappt es beim nächsten Start.
func CleanupOld() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}
