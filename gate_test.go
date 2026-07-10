package main

import (
	"reflect"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func approver(id int64, username string) *gitlab.MergeRequestApproverUser {
	return &gitlab.MergeRequestApproverUser{
		User: &gitlab.BasicUser{ID: id, Username: username, Name: username},
	}
}

func TestEvaluateApprovals(t *testing.T) {
	const authorID = int64(10)

	tests := []struct {
		name        string
		approvedBy  []*gitlab.MergeRequestApproverUser
		countAuthor bool
		required    int64
		wantCount   int64
		wantMet     bool
		wantState   gitlab.BuildStateValue
		wantDesc    string
		wantNames   []string
	}{
		{
			name:      "no approvals",
			required:  2,
			wantCount: 0, wantMet: false, wantState: gitlab.Failed, wantDesc: "0/2 approvals",
		},
		{
			name:       "below threshold",
			approvedBy: []*gitlab.MergeRequestApproverUser{approver(20, "alice")},
			required:   2,
			wantCount:  1, wantMet: false, wantState: gitlab.Failed, wantDesc: "1/2 approvals",
			wantNames: []string{"alice"},
		},
		{
			name:       "meets threshold",
			approvedBy: []*gitlab.MergeRequestApproverUser{approver(20, "alice"), approver(30, "bob")},
			required:   2,
			wantCount:  2, wantMet: true, wantState: gitlab.Success, wantDesc: "2/2 approvals",
			wantNames: []string{"alice", "bob"},
		},
		{
			name:       "author excluded by default",
			approvedBy: []*gitlab.MergeRequestApproverUser{approver(authorID, "author"), approver(20, "alice")},
			required:   2,
			wantCount:  1, wantMet: false, wantState: gitlab.Failed, wantDesc: "1/2 approvals",
			wantNames: []string{"alice"},
		},
		{
			name:        "author counted when enabled",
			approvedBy:  []*gitlab.MergeRequestApproverUser{approver(authorID, "author"), approver(20, "alice")},
			countAuthor: true,
			required:    2,
			wantCount:   2, wantMet: true, wantState: gitlab.Success, wantDesc: "2/2 approvals",
			wantNames: []string{"author", "alice"},
		},
		{
			name:       "nil entries are skipped",
			approvedBy: []*gitlab.MergeRequestApproverUser{nil, {User: nil}, approver(20, "alice")},
			required:   1,
			wantCount:  1, wantMet: true, wantState: gitlab.Success, wantDesc: "1/1 approvals",
			wantNames: []string{"alice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := evaluateApprovals(tt.approvedBy, authorID, tt.countAuthor, tt.required)
			if res.Count != tt.wantCount || res.Met != tt.wantMet || res.State != tt.wantState || res.Description != tt.wantDesc {
				t.Errorf("evaluateApprovals() = {Count:%d Met:%v State:%s Desc:%q}, want {Count:%d Met:%v State:%s Desc:%q}",
					res.Count, res.Met, res.State, res.Description, tt.wantCount, tt.wantMet, tt.wantState, tt.wantDesc)
			}
			if !reflect.DeepEqual(res.Approvers, tt.wantNames) {
				t.Errorf("evaluateApprovals() approvers = %v, want %v", res.Approvers, tt.wantNames)
			}
		})
	}
}
