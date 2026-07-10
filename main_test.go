package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestGetProjectPathFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{
			name:     "Valid URL",
			url:      "https://gitlab.com/foo/bar/-/merge_requests/123",
			expected: "foo/bar",
			wantErr:  false,
		},
		{
			name:     "Valid URL with subgroups",
			url:      "https://gitlab.com/group/subgroup/project/-/merge_requests/456",
			expected: "group/subgroup/project",
			wantErr:  false,
		},
		{
			name:     "Invalid URL",
			url:      "https://gitlab.com/foo",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "Missing project path",
			url:      "https://gitlab.com/-/merge_requests/1",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getProjectPathFromURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("getProjectPathFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("getProjectPathFromURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasRequiredScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		expected bool
	}{
		{name: "read_api scope", scopes: []string{"read_api"}, expected: true},
		{name: "api scope", scopes: []string{"api"}, expected: true},
		{name: "both among others", scopes: []string{"read_user", "api", "read_repository"}, expected: true},
		{name: "insufficient scopes", scopes: []string{"read_user", "read_repository"}, expected: false},
		{name: "no scopes", scopes: nil, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasRequiredScope(tt.scopes); got != tt.expected {
				t.Errorf("hasRequiredScope(%v) = %v, want %v", tt.scopes, got, tt.expected)
			}
		})
	}
}

func TestValidateTokenScopes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{
			name:    "sufficient scopes",
			status:  http.StatusOK,
			body:    `{"id": 1, "scopes": ["read_api"], "active": true}`,
			wantErr: false,
		},
		{
			name:    "insufficient scopes",
			status:  http.StatusOK,
			body:    `{"id": 1, "scopes": ["read_user"], "active": true}`,
			wantErr: true,
		},
		{
			name:    "invalid token",
			status:  http.StatusUnauthorized,
			body:    `{"message": "401 Unauthorized"}`,
			wantErr: true,
		},
		{
			name:    "endpoint unavailable is only a warning",
			status:  http.StatusNotFound,
			body:    `{"message": "404 Not Found"}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v4/personal_access_tokens/self" {
					t.Errorf("unexpected request path: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL))
			if err != nil {
				t.Fatalf("failed to create client: %s", err)
			}

			err = validateTokenScopes(context.Background(), client)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTokenScopes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractProtocolAndDomain(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{
			name:     "Valid URL",
			url:      "https://gitlab.com/foo/bar/-/merge_requests/123",
			expected: "https://gitlab.com",
			wantErr:  false,
		},
		{
			name:     "Invalid URL",
			url:      "://gitlab.com",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "Missing scheme",
			url:      "gitlab.com/foo/bar/-/merge_requests/123",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "Missing host",
			url:      "https://",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractProtocolAndDomain(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractProtocolAndDomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("extractProtocolAndDomain() = %v, want %v", result, tt.expected)
			}
		})
	}
}
