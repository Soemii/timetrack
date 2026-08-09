package jira

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchPaginated(t *testing.T) {
	page := func(startAt int, total int, issues ...string) string {
		var items []string
		for _, k := range issues {
			items = append(items, fmt.Sprintf(
				`{"key":%q,"fields":{"summary":"Summary %s","project":{"key":%q}}}`,
				k, k, strings.SplitN(k, "-", 2)[0]))
		}
		return fmt.Sprintf(`{"startAt":%d,"maxResults":1,"total":%d,"issues":[%s]}`,
			startAt, total, strings.Join(items, ","))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization: %q", got)
		}
		q := r.URL.Query()
		if !strings.Contains(q.Get("jql"), "assignee = currentUser()") {
			t.Errorf("jql: %q", q.Get("jql"))
		}
		if q.Get("fields") != "summary,project" {
			t.Errorf("fields: %q", q.Get("fields"))
		}
		// Server clampt auf 1 Issue pro Seite — erzwingt die Pagination
		switch q.Get("startAt") {
		case "0":
			fmt.Fprint(w, page(0, 2, "ABC-1"))
		case "1":
			fmt.Fprint(w, page(1, 2, "XYZ-2"))
		default:
			t.Errorf("unerwartetes startAt: %q", q.Get("startAt"))
		}
	}))
	defer srv.Close()

	issues, err := Fetch(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].Key != "ABC-1" || issues[0].ProjectKey != "ABC" ||
		issues[0].Summary != "Summary ABC-1" || issues[1].Key != "XYZ-2" || issues[1].ProjectKey != "XYZ" {
		t.Fatalf("issues: %+v", issues)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := Fetch(srv.URL, "bad"); err == nil || !strings.Contains(err.Error(), "Token") {
		t.Fatalf("401 muss Token-Hinweis geben: %v", err)
	}
}

func TestFetchBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>Login</html>")
	}))
	defer srv.Close()
	if _, err := Fetch(srv.URL, "tok"); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("kaputtes JSON muss Fehler geben: %v", err)
	}
}

func TestFetchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := Fetch(srv.URL, "tok"); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("Non-200 muss Fehler mit Status geben: %v", err)
	}
}
