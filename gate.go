package main

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// gateResult is the outcome of evaluating MR approvals against a threshold.
type gateResult struct {
	Count       int64
	Required    int64
	Met         bool
	Approvers   []string
	State       gitlab.BuildStateValue
	Description string
}

// evaluateApprovals counts the approvals that qualify (optionally excluding
// the MR author's own approval) and maps the outcome to a commit status.
func evaluateApprovals(approvedBy []*gitlab.MergeRequestApproverUser, authorID int64, countAuthor bool, required int64) gateResult {
	res := gateResult{Required: required}
	for _, a := range approvedBy {
		if a == nil || a.User == nil {
			continue
		}
		if !countAuthor && authorID != 0 && a.User.ID == authorID {
			continue
		}
		res.Count++
		res.Approvers = append(res.Approvers, a.User.Username)
	}
	res.Met = res.Count >= required
	res.State = gitlab.Failed
	if res.Met {
		res.State = gitlab.Success
	}
	res.Description = fmt.Sprintf("%d/%d approvals", res.Count, required)
	return res
}
