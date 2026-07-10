package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const (
	checkModeApprovals = "approvals"
	checkModeUpvotes   = "upvotes"
)

var (
	checkToken       string
	checkMode        string
	checkApprovals   int64
	checkUpvotes     int64
	checkCountAuthor bool
	checkMRID        int64
	checkPullURL     string

	checkCmd = &cobra.Command{
		Use:   "check",
		Short: "One-shot gate: exit 0 if the MR has enough approvals (or upvotes)",
		Run: func(cmd *cobra.Command, args []string) {
			runCheck()
		},
	}
)

func init() {
	checkCmd.Flags().StringVar(&checkToken, "token", getEnv("GITLAB_TOKEN", ""), "GitLab token (or set GITLAB_TOKEN env var)")
	checkCmd.Flags().StringVar(&checkMode, "mode", getEnv("CHECK_MODE", checkModeApprovals), "What to count: 'approvals' or 'upvotes' (or set CHECK_MODE env var)")
	checkCmd.Flags().Int64Var(&checkApprovals, "approvals", getInt64FromEnv("APPROVALS", 0), "Approvals threshold (or set APPROVALS env var)")
	checkCmd.Flags().Int64Var(&checkUpvotes, "upvotes", getInt64FromEnv("UPVOTES", 0), "Threshold, legacy alias of --approvals (or set UPVOTES env var)")
	checkCmd.Flags().BoolVar(&checkCountAuthor, "count-author-approval", getBoolFromEnv("COUNT_AUTHOR_APPROVAL", false), "Count the MR author's own approval (or set COUNT_AUTHOR_APPROVAL env var)")
	checkCmd.Flags().StringVar(&checkPullURL, "url", getEnv("PULL_URL", ""), "Merge request URL (or set PULL_URL env var)")
	checkCmd.Flags().Int64Var(&checkMRID, "pull-num", getInt64FromEnv("PULL_NUM", 0), "Merge request ID (or set PULL_NUM env var)")

	rootCmd.AddCommand(checkCmd)
}

// checkThreshold resolves the effective threshold: --approvals wins, the
// legacy --upvotes is honoured next, and the default is 1.
func checkThreshold() int64 {
	if checkApprovals > 0 {
		return checkApprovals
	}
	if checkUpvotes > 0 {
		return checkUpvotes
	}
	return 1
}

func runCheck() {
	threshold := checkThreshold()
	if checkApprovals < 0 || checkUpvotes < 0 {
		log.Fatalf("Invalid threshold: must be ≥1 (got %d)", min(checkApprovals, checkUpvotes))
	}
	if checkMode != checkModeApprovals && checkMode != checkModeUpvotes {
		log.Fatalf("Invalid mode %q: must be '%s' or '%s'", checkMode, checkModeApprovals, checkModeUpvotes)
	}
	if checkMRID <= 0 {
		log.Fatalf("Invalid merge request ID: must be positive (got %d)", checkMRID)
	}
	if checkToken == "" {
		log.Fatalf("GitLab token is required. Use --token or set GITLAB_TOKEN environment variable.")
	}
	if checkPullURL == "" {
		log.Fatalf("Merge request URL is required. Use --url or set PULL_URL environment variable.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	gitlabBaseURL, err := extractProtocolAndDomain(checkPullURL)
	if err != nil {
		log.Fatalf("Error extracting base URL: %s", err)
	}

	client, err := newGitlabClient(checkToken, gitlabBaseURL)
	if err != nil {
		log.Fatalf("Failed to create GitLab client: %s", err)
	}

	if err := validateTokenScopes(ctx, client, "api", "read_api"); err != nil {
		log.Fatalf("Token validation failed: %s", err)
	}

	projectPath, err := getProjectPathFromURL(checkPullURL)
	if err != nil {
		log.Fatalf("Failed to extract project path from URL: %s", err)
	}

	projectID, err := getNumericProjectID(ctx, client, projectPath)
	if err != nil {
		log.Fatalf("Failed to get numeric project ID: %s", err)
	}

	log.Printf("Numeric Project ID: %d", projectID)

	mr, _, err := client.MergeRequests.GetMergeRequest(projectID, checkMRID, nil, gitlab.WithContext(ctx))
	if err != nil {
		log.Fatalf("Failed to get MR: %s", err)
	}

	log.Printf("Merge request Author: %s (%s)", mr.Author.Name, mr.Author.Username)

	switch checkMode {
	case checkModeUpvotes:
		if mr.Upvotes >= threshold {
			log.Printf("Merge request has sufficient upvotes (%d/%d).", mr.Upvotes, threshold)
			os.Exit(0)
		}
		log.Printf("Merge request has insufficient upvotes (%d/%d).", mr.Upvotes, threshold)
		os.Exit(1)
	case checkModeApprovals:
		approvals, _, err := client.MergeRequestApprovals.GetConfiguration(projectID, checkMRID, gitlab.WithContext(ctx))
		if err != nil {
			log.Fatalf("Failed to get MR approvals: %s", err)
		}
		res := evaluateApprovals(approvals.ApprovedBy, mr.Author.ID, checkCountAuthor, threshold)
		if len(res.Approvers) > 0 {
			log.Printf("Approved by: %s", strings.Join(res.Approvers, ", "))
		}
		if res.Met {
			log.Printf("Merge request has sufficient approvals (%s).", res.Description)
			os.Exit(0)
		}
		log.Printf("Merge request has insufficient approvals (%s).", res.Description)
		os.Exit(1)
	}
}

// getProjectPathFromURL extracts the full project path (namespace/project,
// including subgroups) from a merge request URL such as
// https://gitlab.example.com/group/subgroup/project/-/merge_requests/1.
func getProjectPathFromURL(mergeRequestURL string) (string, error) {
	u, err := url.Parse(mergeRequestURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	pathSegments := strings.Split(u.Path, "/")

	mergeRequestsIndex := -1
	for i, segment := range pathSegments {
		if segment == "-" && i+1 < len(pathSegments) && pathSegments[i+1] == "merge_requests" {
			mergeRequestsIndex = i
			break
		}
	}

	if mergeRequestsIndex == -1 {
		return "", fmt.Errorf("invalid URL format: missing '-/merge_requests/'")
	}

	projectPath := strings.Trim(strings.Join(pathSegments[:mergeRequestsIndex], "/"), "/")
	if projectPath == "" {
		return "", fmt.Errorf("invalid URL format: missing project path")
	}
	return projectPath, nil
}

func getNumericProjectID(ctx context.Context, client *gitlab.Client, projectPath string) (int64, error) {
	project, _, err := client.Projects.GetProject(projectPath, nil, gitlab.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("failed to fetch project: %w", err)
	}
	return project.ID, nil
}
