package github

import (
	"net/http"
	"net/http/httptest"
	"testing"

	githubapi "github.com/google/go-github/v88/github"
)

func TestListOwnerRepositoriesUsesUserEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", request.Method, http.MethodGet)
		}
		if request.URL.Path != "/api/v3/users/example/repos" {
			t.Errorf("request path = %q, want %q", request.URL.Path, "/api/v3/users/example/repos")
		}
		assertRepositoryListByUserQuery(t, request, "17")
		writer.Header().Set("Content-Type", "application/json")
		if _, err := writer.Write([]byte(`[{"id":1,"name":"repository"}]`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	repositories, err := listOwnerRepositories(t.Context(), newTestGitHubClient(t, server), "example", "user", 17)
	if err != nil {
		t.Fatalf("listOwnerRepositories() error = %v", err)
	}
	if len(repositories) != 1 || repositories[0].GetName() != "repository" {
		t.Fatalf("listOwnerRepositories() repositories = %#v, want one repository", repositories)
	}
}

func TestListOwnerRepositoriesFallsBackFromOrganizationNotFoundToUser(t *testing.T) {
	paths := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths <- request.URL.Path
		switch request.URL.Path {
		case "/api/v3/orgs/example/repos":
			if request.Method != http.MethodGet {
				t.Errorf("organization request method = %q, want %q", request.Method, http.MethodGet)
			}
			query := request.URL.Query()
			if query.Get("type") != "all" || query.Get("per_page") != "23" {
				t.Errorf("organization query = %q, want type=all and per_page=23", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusNotFound)
			if _, err := writer.Write([]byte(`{"message":"Not Found"}`)); err != nil {
				t.Errorf("write organization response: %v", err)
			}
		case "/api/v3/users/example/repos":
			if request.Method != http.MethodGet {
				t.Errorf("user request method = %q, want %q", request.Method, http.MethodGet)
			}
			assertRepositoryListByUserQuery(t, request, "23")
			writer.Header().Set("Content-Type", "application/json")
			if _, err := writer.Write([]byte(`[{"id":2,"name":"fallback-repository"}]`)); err != nil {
				t.Errorf("write user response: %v", err)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	repositories, err := listOwnerRepositories(t.Context(), newTestGitHubClient(t, server), "example", "", 23)
	if err != nil {
		t.Fatalf("listOwnerRepositories() error = %v", err)
	}
	if len(repositories) != 1 || repositories[0].GetName() != "fallback-repository" {
		t.Fatalf("listOwnerRepositories() repositories = %#v, want fallback repository", repositories)
	}
	wantPaths := []string{"/api/v3/orgs/example/repos", "/api/v3/users/example/repos"}
	if len(paths) != len(wantPaths) {
		t.Fatalf("request count = %d, want %d", len(paths), len(wantPaths))
	}
	gotPaths := []string{<-paths, <-paths}
	for index := range wantPaths {
		if gotPaths[index] != wantPaths[index] {
			t.Fatalf("request paths = %v, want %v", gotPaths, wantPaths)
		}
	}
}

func newTestGitHubClient(t *testing.T, server *httptest.Server) *githubapi.Client {
	t.Helper()
	client, err := githubapi.NewClient(
		githubapi.WithHTTPClient(server.Client()),
		githubapi.WithEnterpriseURLs(server.URL+"/api/v3/", server.URL+"/api/uploads/"),
	)
	if err != nil {
		t.Fatalf("create GitHub client: %v", err)
	}
	return client
}

func assertRepositoryListByUserQuery(t *testing.T, request *http.Request, perPage string) {
	t.Helper()
	query := request.URL.Query()
	if query.Get("type") != "all" {
		t.Errorf("query type = %q, want %q", query.Get("type"), "all")
	}
	if query.Get("sort") != "pushed" {
		t.Errorf("query sort = %q, want %q", query.Get("sort"), "pushed")
	}
	if query.Get("direction") != "desc" {
		t.Errorf("query direction = %q, want %q", query.Get("direction"), "desc")
	}
	if query.Get("per_page") != perPage {
		t.Errorf("query per_page = %q, want %q", query.Get("per_page"), perPage)
	}
}
