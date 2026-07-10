package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// statusContextName is the commit status name shown in the MR pipeline widget.
const statusContextName = "approve-guard"

var (
	serveListen      string
	serveSecret      string
	serveGitLabURL   string
	serveToken       string
	serveThreshold   int64
	serveCountAuthor bool
	serveResetOnPush bool

	serveCmd = &cobra.Command{
		Use:   "serve",
		Short: "Webhook server: gate the Merge button via commit statuses",
		Long: "Listens for GitLab merge request webhooks and posts a commit status\n" +
			"('" + statusContextName + "') of success/failed depending on whether the MR has\n" +
			"enough approvals. Combine with the 'Pipelines must succeed' merge check\n" +
			"to block the Merge button on GitLab Free.",
		Run: func(cmd *cobra.Command, args []string) {
			runServe()
		},
	}
)

func init() {
	serveCmd.Flags().StringVar(&serveListen, "listen", getEnv("LISTEN_ADDR", ":8080"), "Address to listen on (or set LISTEN_ADDR env var)")
	serveCmd.Flags().StringVar(&serveSecret, "webhook-secret", getEnv("GITLAB_WEBHOOK_SECRET", ""), "Webhook secret token (or set GITLAB_WEBHOOK_SECRET env var)")
	serveCmd.Flags().StringVar(&serveGitLabURL, "gitlab-url", getEnv("GITLAB_URL", ""), "GitLab base URL; derived from the webhook payload when empty (or set GITLAB_URL env var)")
	serveCmd.Flags().StringVar(&serveToken, "token", getEnv("GITLAB_TOKEN", ""), "GitLab token with 'api' scope (or set GITLAB_TOKEN env var)")
	serveCmd.Flags().Int64Var(&serveThreshold, "approvals", getInt64FromEnv("APPROVALS", 1), "Default approvals threshold; override per webhook with ?approvals=N (or set APPROVALS env var)")
	serveCmd.Flags().BoolVar(&serveCountAuthor, "count-author-approval", getBoolFromEnv("COUNT_AUTHOR_APPROVAL", false), "Count the MR author's own approval (or set COUNT_AUTHOR_APPROVAL env var)")
	serveCmd.Flags().BoolVar(&serveResetOnPush, "reset-approvals-on-push", getBoolFromEnv("RESET_APPROVALS_ON_PUSH", true), "Reset approvals when new commits are pushed; requires a project/group bot token (or set RESET_APPROVALS_ON_PUSH env var)")

	rootCmd.AddCommand(serveCmd)
}

type server struct {
	token            string
	baseURLOverride  string
	secret           []byte
	defaultThreshold int64
	countAuthor      bool
	resetOnPush      bool

	mu      sync.Mutex
	clients map[string]*gitlab.Client
}

func runServe() {
	if serveSecret == "" {
		log.Fatalf("Webhook secret is required. Use --webhook-secret or set GITLAB_WEBHOOK_SECRET environment variable.")
	}
	if serveToken == "" {
		log.Fatalf("GitLab token is required. Use --token or set GITLAB_TOKEN environment variable.")
	}
	if serveThreshold < 1 {
		log.Fatalf("Invalid approvals threshold: must be ≥1 (got %d)", serveThreshold)
	}

	s := &server{
		token:            serveToken,
		baseURLOverride:  serveGitLabURL,
		secret:           []byte(serveSecret),
		defaultThreshold: serveThreshold,
		countAuthor:      serveCountAuthor,
		resetOnPush:      serveResetOnPush,
		clients:          map[string]*gitlab.Client{},
	}

	// With a fixed GitLab URL the token can be validated up front; otherwise
	// the base URL is only known once the first webhook arrives.
	if s.baseURLOverride != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		client, err := s.client("")
		if err == nil {
			err = validateTokenScopes(ctx, client, "api")
		}
		cancel()
		if err != nil {
			log.Fatalf("Token validation failed: %s", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", s.handleWebhook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	httpServer := &http.Server{
		Addr:              serveListen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("ApproveGuard %s listening on %s (default threshold: %d approvals)", version, serveListen, serveThreshold)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Fatalf("Server failed: %s", err)
	case <-ctx.Done():
		log.Println("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("Shutdown failed: %s", err)
		}
	}
}

func (s *server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), s.secret) != 1 {
		http.Error(w, "invalid webhook token", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var event mergeRequestEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	if event.ObjectKind != "merge_request" {
		fmt.Fprintf(w, "ignored: object_kind %q\n", event.ObjectKind)
		return
	}

	threshold := s.defaultThreshold
	if q := r.URL.Query().Get("approvals"); q != "" {
		n, err := strconv.ParseInt(q, 10, 64)
		if err != nil || n < 1 {
			http.Error(w, "invalid 'approvals' query parameter: must be a positive integer", http.StatusBadRequest)
			return
		}
		threshold = n
	}

	result, err := s.process(r.Context(), &event, threshold)
	if err != nil {
		log.Printf("Error processing webhook: %s", err)
		http.Error(w, "processing failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, result)
}

func (s *server) process(ctx context.Context, event *mergeRequestEvent, required int64) (string, error) {
	attrs := &event.ObjectAttributes
	mrRef := fmt.Sprintf("%s!%d", event.Project.PathWithNamespace, attrs.IID)

	if !relevantActions[attrs.Action] {
		return fmt.Sprintf("ignored: action %q", attrs.Action), nil
	}
	if attrs.State != "opened" {
		return fmt.Sprintf("ignored: %s is %s", mrRef, attrs.State), nil
	}
	sha := attrs.LastCommit.ID
	if sha == "" {
		return "", fmt.Errorf("%s: payload has no last_commit id", mrRef)
	}

	client, err := s.client(event.Project.WebURL)
	if err != nil {
		return "", err
	}

	// New commits arrive as an "update" action carrying oldrev. The stale
	// approvals are dropped before re-evaluating, mirroring the Premium
	// "reset approvals on push" behaviour.
	if s.resetOnPush && attrs.Action == "update" && attrs.OldRev != "" {
		if _, err := client.MergeRequestApprovals.ResetApprovalsOfMergeRequest(event.Project.ID, attrs.IID, gitlab.WithContext(ctx)); err != nil {
			log.Printf("%s: could not reset approvals after push (the endpoint requires a project/group bot token): %s", mrRef, err)
		} else {
			log.Printf("%s: approvals reset after new commits", mrRef)
		}
	}

	approvals, _, err := client.MergeRequestApprovals.GetConfiguration(event.Project.ID, attrs.IID, gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("%s: failed to get approvals: %w", mrRef, err)
	}

	res := evaluateApprovals(approvals.ApprovedBy, attrs.AuthorID, s.countAuthor, required)

	opts := &gitlab.SetCommitStatusOptions{
		State:       res.State,
		Name:        gitlab.Ptr(statusContextName),
		Description: gitlab.Ptr(res.Description),
	}
	if attrs.SourceBranch != "" {
		opts.Ref = gitlab.Ptr(attrs.SourceBranch)
	}
	if attrs.URL != "" {
		opts.TargetURL = gitlab.Ptr(attrs.URL)
	}
	if _, _, err := client.Commits.SetCommitStatus(event.Project.ID, sha, opts, gitlab.WithContext(ctx)); err != nil {
		// Re-posting the same state is rejected by GitLab; that just means
		// the status is already correct.
		if !strings.Contains(err.Error(), "Cannot transition status") {
			return "", fmt.Errorf("%s: failed to set commit status: %w", mrRef, err)
		}
	}

	msg := fmt.Sprintf("%s @ %.8s: %s (%s)", mrRef, sha, res.Description, res.State)
	if len(res.Approvers) > 0 {
		msg += " approved by: " + strings.Join(res.Approvers, ", ")
	}
	log.Println(msg)
	return msg, nil
}

// client returns a GitLab API client for the instance the event came from
// (or the fixed --gitlab-url), creating and caching it on first use.
func (s *server) client(projectWebURL string) (*gitlab.Client, error) {
	base := s.baseURLOverride
	if base == "" {
		derived, err := extractProtocolAndDomain(projectWebURL)
		if err != nil {
			return nil, fmt.Errorf("cannot derive GitLab base URL from project web_url %q: %w", projectWebURL, err)
		}
		base = derived
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[base]; ok {
		return c, nil
	}
	c, err := newGitlabClient(s.token, base)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitLab client for %s: %w", base, err)
	}
	s.clients[base] = c
	return c, nil
}
