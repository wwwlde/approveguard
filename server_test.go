package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// mockGitLab is a fake GitLab API recording approval fetches, status posts
// and approval resets.
type mockGitLab struct {
	mu             sync.Mutex
	approvedBy     []map[string]any // approved_by payload returned by GET approvals
	statusPosts    []map[string]any // bodies of POST /statuses/:sha
	resetCalls     int
	statusPostCode int    // HTTP code for status posts (default 201)
	statusPostBody string // body for non-2xx status posts
}

func (m *mockGitLab) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/77/merge_requests/5/approvals", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": 5, "iid": 5, "project_id": 77,
			"approved_by": m.approvedBy,
		})
	})
	mux.HandleFunc("PUT /api/v4/projects/77/merge_requests/5/reset_approvals", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.resetCalls++
		m.approvedBy = nil // resetting drops all approvals
		m.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v4/projects/77/statuses/{sha}", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		json.Unmarshal(body, &payload)
		payload["sha"] = r.PathValue("sha")
		m.statusPosts = append(m.statusPosts, payload)
		if m.statusPostCode != 0 && m.statusPostCode != http.StatusCreated {
			w.WriteHeader(m.statusPostCode)
			fmt.Fprint(w, m.statusPostBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id": 1, "status": "success"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected GitLab API request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	return mux
}

func user(id int64, username string) map[string]any {
	return map[string]any{"user": map[string]any{"id": id, "username": username, "name": username}}
}

func mrPayload(action, state, oldRev string) string {
	payload := map[string]any{
		"object_kind": "merge_request",
		"project": map[string]any{
			"id": 77, "web_url": "https://gitlab.example.com/foo/bar", "path_with_namespace": "foo/bar",
		},
		"object_attributes": map[string]any{
			"iid": 5, "action": action, "state": state,
			"url":           "https://gitlab.example.com/foo/bar/-/merge_requests/5",
			"source_branch": "feature",
			"author_id":     10,
			"oldrev":        oldRev,
			"last_commit":   map[string]any{"id": "abc123def456"},
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

const testSecret = "s3cret"

// newTestSetup wires a webhook server instance to a mock GitLab API.
func newTestSetup(t *testing.T, mock *mockGitLab, threshold int64, countAuthor, resetOnPush bool) *httptest.Server {
	t.Helper()
	gitlabSrv := httptest.NewServer(mock.handler(t))
	t.Cleanup(gitlabSrv.Close)

	s := &server{
		token:            "test-token",
		baseURLOverride:  gitlabSrv.URL,
		secret:           []byte(testSecret),
		defaultThreshold: threshold,
		countAuthor:      countAuthor,
		resetOnPush:      resetOnPush,
		clients:          map[string]*gitlab.Client{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", s.handleWebhook)
	hookSrv := httptest.NewServer(mux)
	t.Cleanup(hookSrv.Close)
	return hookSrv
}

func postWebhook(t *testing.T, url, secret, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to build request: %s", err)
	}
	req.Header.Set("X-Gitlab-Token", secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %s", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func lastStatus(t *testing.T, mock *mockGitLab) map[string]any {
	t.Helper()
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.statusPosts) == 0 {
		t.Fatalf("expected a commit status to be posted, got none")
	}
	return mock.statusPosts[len(mock.statusPosts)-1]
}

func TestWebhookRejectsBadSecret(t *testing.T) {
	mock := &mockGitLab{}
	srv := newTestSetup(t, mock, 1, false, false)

	resp := postWebhook(t, srv.URL+"/webhook", "wrong", mrPayload("approval", "opened", ""))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if len(mock.statusPosts) != 0 {
		t.Errorf("no GitLab calls expected on bad secret")
	}
}

func TestWebhookIgnoresNonMREvents(t *testing.T) {
	mock := &mockGitLab{}
	srv := newTestSetup(t, mock, 1, false, false)

	resp := postWebhook(t, srv.URL+"/webhook", testSecret, `{"object_kind": "push"}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ignored") {
		t.Errorf("expected 'ignored' response, got %q", body)
	}
}

func TestWebhookIgnoresClosedMR(t *testing.T) {
	mock := &mockGitLab{}
	srv := newTestSetup(t, mock, 1, false, false)

	resp := postWebhook(t, srv.URL+"/webhook", testSecret, mrPayload("update", "merged", ""))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if len(mock.statusPosts) != 0 {
		t.Errorf("no status expected for merged MR")
	}
}

func TestWebhookPostsSuccessWhenThresholdMet(t *testing.T) {
	mock := &mockGitLab{approvedBy: []map[string]any{user(20, "alice"), user(30, "bob")}}
	srv := newTestSetup(t, mock, 2, false, false)

	resp := postWebhook(t, srv.URL+"/webhook", testSecret, mrPayload("approval", "opened", ""))
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	status := lastStatus(t, mock)
	if status["state"] != "success" {
		t.Errorf("state = %v, want success", status["state"])
	}
	if status["description"] != "2/2 approvals" {
		t.Errorf("description = %v, want '2/2 approvals'", status["description"])
	}
	if status["name"] != statusContextName {
		t.Errorf("name = %v, want %s", status["name"], statusContextName)
	}
	if status["sha"] != "abc123def456" {
		t.Errorf("sha = %v, want abc123def456", status["sha"])
	}
}

func TestWebhookExcludesAuthorApproval(t *testing.T) {
	// author (id 10) approved their own MR; only alice counts
	mock := &mockGitLab{approvedBy: []map[string]any{user(10, "author"), user(20, "alice")}}
	srv := newTestSetup(t, mock, 2, false, false)

	postWebhook(t, srv.URL+"/webhook", testSecret, mrPayload("approval", "opened", ""))
	status := lastStatus(t, mock)
	if status["state"] != "failed" || status["description"] != "1/2 approvals" {
		t.Errorf("got state=%v desc=%v, want failed '1/2 approvals'", status["state"], status["description"])
	}
}

func TestWebhookThresholdQueryOverride(t *testing.T) {
	mock := &mockGitLab{approvedBy: []map[string]any{user(20, "alice")}}
	srv := newTestSetup(t, mock, 2, false, false)

	postWebhook(t, srv.URL+"/webhook?approvals=1", testSecret, mrPayload("approval", "opened", ""))
	status := lastStatus(t, mock)
	if status["state"] != "success" || status["description"] != "1/1 approvals" {
		t.Errorf("got state=%v desc=%v, want success '1/1 approvals'", status["state"], status["description"])
	}
}

func TestWebhookResetsApprovalsOnPush(t *testing.T) {
	mock := &mockGitLab{approvedBy: []map[string]any{user(20, "alice"), user(30, "bob")}}
	srv := newTestSetup(t, mock, 2, false, true)

	// update with oldrev = new commits pushed
	postWebhook(t, srv.URL+"/webhook", testSecret, mrPayload("update", "opened", "deadbeef"))
	if mock.resetCalls != 1 {
		t.Errorf("resetCalls = %d, want 1", mock.resetCalls)
	}
	status := lastStatus(t, mock)
	if status["state"] != "failed" || status["description"] != "0/2 approvals" {
		t.Errorf("got state=%v desc=%v, want failed '0/2 approvals' after reset", status["state"], status["description"])
	}
}

func TestWebhookNoResetOnPlainUpdate(t *testing.T) {
	mock := &mockGitLab{approvedBy: []map[string]any{user(20, "alice")}}
	srv := newTestSetup(t, mock, 1, false, true)

	// update without oldrev (e.g. title change) must not reset approvals
	postWebhook(t, srv.URL+"/webhook", testSecret, mrPayload("update", "opened", ""))
	if mock.resetCalls != 0 {
		t.Errorf("resetCalls = %d, want 0", mock.resetCalls)
	}
	status := lastStatus(t, mock)
	if status["state"] != "success" {
		t.Errorf("state = %v, want success", status["state"])
	}
}

func TestWebhookToleratesStatusTransitionConflict(t *testing.T) {
	mock := &mockGitLab{
		approvedBy:     []map[string]any{user(20, "alice")},
		statusPostCode: http.StatusBadRequest,
		statusPostBody: `{"message": "Cannot transition status via :enqueue from :pending"}`,
	}
	srv := newTestSetup(t, mock, 1, false, false)

	resp := postWebhook(t, srv.URL+"/webhook", testSecret, mrPayload("approval", "opened", ""))
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 200 — transition conflicts are not errors (body: %s)", resp.StatusCode, body)
	}
}
