package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	version = "dev" // Default version if not set during build
	rootCmd = &cobra.Command{
		Use:   "approve-guard",
		Short: "GitLab MR approval gate for GitLab Free",
		Long: "ApproveGuard gates merges on GitLab Free by counting MR approvals.\n" +
			"Run 'check' as a one-shot gate (e.g. in an Atlantis workflow) or 'serve'\n" +
			"as a webhook server that blocks the Merge button via commit statuses.",
		Version: version,
	}
)

func init() {
	rootCmd.Use = filepath.Base(os.Args[0])
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error: %s", err)
	}
}

func newGitlabClient(token, baseURL string) (*gitlab.Client, error) {
	return gitlab.NewClient(
		token,
		gitlab.WithBaseURL(baseURL),
		gitlab.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
}

// validateTokenScopes checks that the supplied token carries one of the
// accepted scopes. Token types that do not expose
// GET /personal_access_tokens/self (e.g. OAuth tokens, or GitLab versions
// without the endpoint) only produce a warning — the real API calls that
// follow will surface any genuine permission problem.
func validateTokenScopes(ctx context.Context, client *gitlab.Client, accepted ...string) error {
	token, resp, err := client.PersonalAccessTokens.GetSinglePersonalAccessToken(gitlab.WithContext(ctx))
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("token is invalid, revoked or expired: %w", err)
		}
		log.Printf("Warning: could not verify token scopes: %s. Continuing anyway.", err)
		return nil
	}
	if !hasAnyScope(token.Scopes, accepted) {
		return fmt.Errorf("token lacks a required scope (need one of %v), got scopes: %v", accepted, token.Scopes)
	}
	return nil
}

func hasAnyScope(scopes []string, accepted []string) bool {
	for _, scope := range scopes {
		for _, want := range accepted {
			if scope == want {
				return true
			}
		}
	}
	return false
}

func extractProtocolAndDomain(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("invalid URL %q: scheme and host are required", rawURL)
	}
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host), nil
}

func getInt64FromEnv(envVar string, defaultValue int64) int64 {
	valueStr := os.Getenv(envVar)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		log.Printf("Could not convert env var '%s' to int: %s", envVar, err)
		return defaultValue
	}
	return value
}

func getBoolFromEnv(envVar string, defaultValue bool) bool {
	valueStr := os.Getenv(envVar)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		log.Printf("Could not convert env var '%s' to bool: %s", envVar, err)
		return defaultValue
	}
	return value
}

func getEnv(envVar, defaultValue string) string {
	value := os.Getenv(envVar)
	if value == "" {
		return defaultValue
	}
	return value
}
