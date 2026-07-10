package main

import (
	"testing"
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
