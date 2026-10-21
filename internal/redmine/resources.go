package redmine

import (
	"context"
	"encoding/json"
	"net/url"
)

// IDName is Redmine's usual {id, name} reference.
type IDName struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// User is the subset of /users/current.json the planner needs.
type User struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Admin     bool   `json:"admin"`
	CreatedOn string `json:"created_on"`
}

// CurrentUser checks authentication against /users/current.json.
func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var out struct {
		User User `json:"user"`
	}
	err := c.getJSON(ctx, "/users/current.json", nil, &out)
	return out.User, err
}

// Project is one entry from /projects.json.
type Project struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Identifier string  `json:"identifier"`
	Status     int     `json:"status"`
	IsPublic   *bool   `json:"is_public"`
	Parent     *IDName `json:"parent"`
	CreatedOn  string  `json:"created_on"`
	UpdatedOn  string  `json:"updated_on"`
}

// ListProjects returns every project visible to the API key.
func (c *Client) ListProjects(ctx context.Context) ([]Project, PageStats, error) {
	var all []Project
	stats, err := c.fetchAll(ctx, "/projects.json", nil, func(body json.RawMessage) (page, []int, error) {
		var p struct {
			page
			Projects []Project `json:"projects"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return page{}, nil, err
		}
		ids := make([]int, len(p.Projects))
		for i, pr := range p.Projects {
			ids[i] = pr.ID
		}
		all = append(all, p.Projects...)
		return p.page, ids, nil
	})
	return all, stats, err
}

// IssueStatus is one entry from /issue_statuses.json.
type IssueStatus struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsClosed bool   `json:"is_closed"`
}

// ListIssueStatuses returns all issue statuses, used to tell open from closed.
func (c *Client) ListIssueStatuses(ctx context.Context) ([]IssueStatus, error) {
	var out struct {
		IssueStatuses []IssueStatus `json:"issue_statuses"`
	}
	err := c.getJSON(ctx, "/issue_statuses.json", nil, &out)
	return out.IssueStatuses, err
}

// Issue is the subset of an /issues.json entry used for discovery.
// Pointer fields are nil when Redmine leaves the value out, so a missing
// estimate or date stays unknown instead of becoming zero.
type Issue struct {
	ID           int     `json:"id"`
	Project      IDName  `json:"project"`
	Tracker      IDName  `json:"tracker"`
	Status       IDName  `json:"status"`
	Priority     IDName  `json:"priority"`
	AssignedTo   *IDName `json:"assigned_to"`
	FixedVersion *IDName `json:"fixed_version"`
	Parent       *struct {
		ID int `json:"id"`
	} `json:"parent"`
	StartDate      *string  `json:"start_date"`
	DueDate        *string  `json:"due_date"`
	DoneRatio      *int     `json:"done_ratio"`
	EstimatedHours *float64 `json:"estimated_hours"`
	SpentHours     *float64 `json:"spent_hours"`
	CreatedOn      string   `json:"created_on"`
	UpdatedOn      string   `json:"updated_on"`
	ClosedOn       *string  `json:"closed_on"`
	IsPrivate      *bool    `json:"is_private"`
}

// ListIssues returns every issue visible to the API key, open and closed.
// extra adds filters such as project_id; status_id=* and sort=id are always
// set so closed issues are included and offset pagination stays stable.
func (c *Client) ListIssues(ctx context.Context, extra url.Values) ([]Issue, PageStats, error) {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("status_id", "*")
	q.Set("sort", "id")

	var all []Issue
	stats, err := c.fetchAll(ctx, "/issues.json", q, func(body json.RawMessage) (page, []int, error) {
		var p struct {
			page
			Issues []Issue `json:"issues"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return page{}, nil, err
		}
		ids := make([]int, len(p.Issues))
		for i, is := range p.Issues {
			ids[i] = is.ID
		}
		all = append(all, p.Issues...)
		return p.page, ids, nil
	})
	return all, stats, err
}
