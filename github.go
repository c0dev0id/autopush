package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	pollInterval  = 15 * time.Second
	appearTimeout = 60 * time.Second
)

type workflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

type runsResponse struct {
	WorkflowRuns []workflowRun `json:"workflow_runs"`
}

// watchWorkflows polls the GitHub Actions API for workflow runs triggered by
// sha. It waits up to appearTimeout for runs to appear, then polls until all
// runs reach a terminal state. Exits immediately when ctx is cancelled.
func watchWorkflows(ctx context.Context, owner, repo, sha, token string) {
	client := &http.Client{Timeout: 15 * time.Second}

	// Wait for runs to appear; they may take a few seconds to register after push.
	var runs []workflowRun
	deadline := time.Now().Add(appearTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
		r, err := fetchRuns(ctx, client, owner, repo, sha, token)
		if err == nil && len(r) > 0 {
			runs = r
			break
		}
	}
	if len(runs) == 0 {
		return // no workflows triggered for this commit
	}

	notify(fmt.Sprintf("watching %d workflow(s)...", len(runs)))

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}

		runs, err := fetchRuns(ctx, client, owner, repo, sha, token)
		if err != nil {
			continue
		}

		allDone := true
		anyFailed := false
		for _, r := range runs {
			if r.Status != "completed" {
				allDone = false
				continue
			}
			switch r.Conclusion {
			case "success", "skipped", "neutral":
				// acceptable
			default:
				anyFailed = true
				notify(fmt.Sprintf("FAILED: %s -- %s", r.Name, r.HTMLURL))
			}
		}

		if allDone {
			if !anyFailed {
				notify("CI passed")
			}
			return
		}
	}
}

func fetchRuns(ctx context.Context, client *http.Client, owner, repo, sha, token string) ([]workflowRun, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/actions/runs?head_sha=%s&per_page=20",
		owner, repo, sha,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API: %s", resp.Status)
	}

	var result runsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.WorkflowRuns, nil
}
