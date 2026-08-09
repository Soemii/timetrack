package service

import (
	"fmt"
	"strings"
	"time"
)

// ponytail: Klartext-PAT in config — Keychain erst, wenn CGO ohnehin kommt.
// Gleiche Exposition wie die geheime outlook_ics_url in derselben Tabelle.
const (
	jiraURLKey   = "jira_base_url"
	jiraTokenKey = "jira_token"      // Personal Access Token (Server/DC)
	jiraErrKey   = "jira_sync_error" // letzter Fetch-Fehler, leer = ok
	jiraThrottle = 5 * time.Minute
)

// JiraSettings ist der Konfigurationsstand für CLI-Anzeige.
// Der Token selbst verlässt den Service nicht.
type JiraSettings struct {
	BaseURL  string
	TokenSet bool
	LastErr  string
}

func (s *Service) JiraSettings() (JiraSettings, error) {
	url, err := s.repo.GetConfig(jiraURLKey)
	if err != nil {
		return JiraSettings{}, err
	}
	token, err := s.repo.GetConfig(jiraTokenKey)
	if err != nil {
		return JiraSettings{}, err
	}
	lastErr, err := s.repo.GetConfig(jiraErrKey)
	if err != nil {
		return JiraSettings{}, err
	}
	return JiraSettings{BaseURL: url, TokenSet: token != "", LastErr: lastErr}, nil
}

// SaveJiraSettings speichert URL und Token. Leere URL schaltet den Import ab.
// Leerer Token bei gesetztem Token bedeutet "behalten" (CLI-Wizard-Konvention).
func (s *Service) SaveJiraSettings(baseURL, token string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return fmt.Errorf("%w: JIRA-URL muss mit http:// oder https:// beginnen", ErrConflict)
	}
	if baseURL == "" {
		token = ""
	} else if token == "" {
		old, err := s.repo.GetConfig(jiraTokenKey)
		if err != nil {
			return err
		}
		token = old
	}
	if err := s.repo.SetConfig(jiraURLKey, baseURL); err != nil {
		return err
	}
	return s.repo.SetConfig(jiraTokenKey, token)
}

// syncJira gleicht die dem Nutzer zugewiesenen JIRA-Issues als Aufgaben ab.
// Issues landen im Projekt mit passendem jira_project_key; nicht gemappte
// werden ignoriert. Verschwundene Issues werden archiviert (Einträge dürfen
// sie weiter referenzieren), wieder auftauchende reaktiviert der Upsert.
// ponytail: Der Archiv-Sweep läuft nur für gemappte Projekte — ein Projekt,
// dessen Key entfernt wird, behält seine alten Aufgaben bis zum Handanlegen.
func (s *Service) syncJira(force bool) error {
	if s.Issues == nil {
		return nil
	}
	baseURL, err := s.repo.GetConfig(jiraURLKey)
	if err != nil || baseURL == "" {
		return err
	}
	token, err := s.repo.GetConfig(jiraTokenKey)
	if err != nil || token == "" {
		return err
	}
	now := s.now()
	if !force && now.Sub(s.lastJiraFetch) < jiraThrottle {
		return nil
	}
	// Vor dem Fetch stempeln: ein kaputter Server retried im Throttle-Takt,
	// nicht bei jedem 2s-Poll der Web-UI.
	s.lastJiraFetch = now

	issues, err := s.Issues(baseURL, token)
	if err != nil {
		return s.repo.SetConfig(jiraErrKey, err.Error()) // best effort
	}

	projects, err := s.repo.Projects(false)
	if err != nil {
		return err
	}
	byKey := map[string]int64{}
	for _, p := range projects {
		if p.JiraKey != "" {
			byKey[strings.ToUpper(p.JiraKey)] = p.ID
		}
	}

	seen := map[int64]map[string]bool{} // projectID → gesehene Issue-Keys
	for _, is := range issues {
		pid, ok := byKey[strings.ToUpper(is.ProjectKey)]
		if !ok {
			continue
		}
		if _, err := s.repo.UpsertJiraTask(pid, is.Key, is.Summary, now); err != nil {
			return err
		}
		if seen[pid] == nil {
			seen[pid] = map[string]bool{}
		}
		seen[pid][is.Key] = true
	}

	for _, pid := range byKey {
		tasks, err := s.repo.TasksForProject(pid, false)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.JiraKey != "" && !seen[pid][t.JiraKey] {
				if err := s.repo.SetTaskArchived(t.ID, true); err != nil {
					return err
				}
			}
		}
	}
	return s.repo.SetConfig(jiraErrKey, "")
}
