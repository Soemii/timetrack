package outlook

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"timetrack/internal/core/ports"
)

// Fetch lädt die ICS-Quelle und liefert Termine mit Ende in (from, to].
func Fetch(url string, from, to time.Time) ([]ports.Meeting, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseICS(string(body), from, to), nil
}
