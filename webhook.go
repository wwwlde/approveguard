package main

// mergeRequestEvent is the subset of the GitLab merge request webhook payload
// that the server needs. Reference:
// https://docs.gitlab.com/user/project/integrations/webhook_events/#merge-request-events
type mergeRequestEvent struct {
	ObjectKind string `json:"object_kind"`
	Project    struct {
		ID                int64  `json:"id"`
		WebURL            string `json:"web_url"`
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes struct {
		IID          int64  `json:"iid"`
		Action       string `json:"action"`
		State        string `json:"state"`
		URL          string `json:"url"`
		SourceBranch string `json:"source_branch"`
		AuthorID     int64  `json:"author_id"`
		// OldRev is only present on "update" actions caused by new commits
		// being pushed to the source branch.
		OldRev     string `json:"oldrev"`
		LastCommit struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

// relevantActions are the merge request webhook actions that can change the
// approval state or the head commit, and therefore require re-evaluation.
var relevantActions = map[string]bool{
	"open":       true,
	"reopen":     true,
	"update":     true,
	"approval":   true,
	"approved":   true,
	"unapproval": true,
	"unapproved": true,
}
