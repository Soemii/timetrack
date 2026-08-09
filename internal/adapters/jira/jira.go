// Package jira lädt die dem Nutzer zugewiesenen Issues von einem
// JIRA Server/Data Center (REST API v2, PAT als Bearer-Token).
package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"timetrack/internal/core/ports"
)

var client = &http.Client{Timeout: 10 * time.Second}

const jql = "assignee = currentUser() AND resolution = Unresolved ORDER BY key"

// maxIssues kappt die Gesamtmenge.
// ponytail: wer mehr als 1000 offene zugewiesene Tickets hat, hat andere Probleme.
const maxIssues = 1000

// Fetch lädt alle dem Nutzer zugewiesenen, ungelösten Issues.
// Paginiert, weil Server/DC maxResults serverseitig clampt (oft auf 50).
func Fetch(baseURL, token string) ([]ports.Issue, error) {
	var out []ports.Issue
	for startAt := 0; startAt < maxIssues; {
		page, err := fetchPage(baseURL, token, startAt)
		if err != nil {
			return nil, err
		}
		for _, is := range page.Issues {
			out = append(out, ports.Issue{
				Key:        is.Key,
				ProjectKey: is.Fields.Project.Key,
				Summary:    is.Fields.Summary,
			})
		}
		startAt += len(page.Issues)
		if len(page.Issues) == 0 || startAt >= page.Total {
			break
		}
	}
	return out, nil
}

type searchPage struct {
	StartAt    int `json:"startAt"`
	MaxResults int `json:"maxResults"`
	Total      int `json:"total"`
	Issues     []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
		} `json:"fields"`
	} `json:"issues"`
}

func fetchPage(baseURL, token string, startAt int) (searchPage, error) {
	q := url.Values{
		"jql":        {jql},
		"fields":     {"summary,project"},
		"maxResults": {"200"},
		"startAt":    {fmt.Sprint(startAt)},
	}
	u := baseURL + "/rest/api/2/search?" + q.Encode()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return searchPage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return searchPage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return searchPage{}, fmt.Errorf("JIRA: 401 Unauthorized — Personal Access Token ungültig oder abgelaufen")
	}
	if resp.StatusCode != http.StatusOK {
		return searchPage{}, fmt.Errorf("JIRA: GET /rest/api/2/search: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return searchPage{}, err
	}
	var page searchPage
	if err := json.Unmarshal(body, &page); err != nil {
		return searchPage{}, fmt.Errorf("JIRA: Antwort kein JSON: %v", err)
	}
	return page, nil
}
