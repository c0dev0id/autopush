package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	appearInterval = 5 * time.Second
	appearTimeout  = 60 * time.Second
	pollInterval   = 10 * time.Second
)

type workflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HTMLURL    string    `json:"html_url"`
	HeadSHA    string    `json:"head_sha"`
	CreatedAt  time.Time `json:"created_at"`
}

type runsResponse struct {
	WorkflowRuns []workflowRun `json:"workflow_runs"`
}

// watchWorkflows polls the GitHub Actions API for workflow runs triggered by
// sha. It waits up to appearTimeout for runs to appear, then polls until all
// runs reach a terminal state. Exits immediately when ctx is cancelled.
// In oneshot mode it returns an exit code (0=ok/no-workflow, 1=error, 2=failed);
// in daemon mode the return value is ignored.
func watchWorkflows(ctx context.Context, owner, repo, sha, token, repoName string, oneshot bool) int {
	client := &http.Client{Timeout: 15 * time.Second}

	// Wait for runs to appear; they may take a few seconds to register after push.
	// Try immediately first (handles the startup/already-pushed case), then poll.
	var runs []workflowRun
	if r, err := fetchRuns(ctx, client, owner, repo, sha, token); err != nil {
		if oneshot {
			notify(repoName, "workflow check failed: "+err.Error())
			return 1
		}
	} else if len(r) > 0 {
		runs = r
	}
	deadline := time.Now().Add(appearTimeout)
	for len(runs) == 0 && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(appearInterval):
		}
		r, err := fetchRuns(ctx, client, owner, repo, sha, token)
		if err != nil {
			if oneshot {
				notify(repoName, "workflow check failed: "+err.Error())
				return 1
			}
			continue
		}
		if len(r) > 0 {
			runs = r
		}
	}
	if len(runs) == 0 {
		recent, err := fetchRecentRuns(ctx, client, owner, repo, token)
		if err != nil || len(recent) == 0 {
			notify(repoName, "no workflow started for "+sha[:8])
			return 0
		}
		for _, r := range recent {
			ts := r.CreatedAt.Local().Format("2006-01-02 15:04")
			notify(repoName, fmt.Sprintf("last run: %-20s  %s  %.8s  [%s]", r.Name, ts, r.HeadSHA, r.Status))
		}
		var inProgress []workflowRun
		for _, r := range recent {
			if r.Status != "completed" {
				inProgress = append(inProgress, r)
			}
		}
		if len(inProgress) == 0 {
			return 0
		}
		notify(repoName, fmt.Sprintf("monitoring %d in-progress workflow(s) from prior commit...", len(inProgress)))
		for {
			select {
			case <-ctx.Done():
				return 0
			case <-time.After(pollInterval):
			}
			allDone := true
			anyFailed := false
			for i := range inProgress {
				if inProgress[i].Status == "completed" {
					switch inProgress[i].Conclusion {
					case "success", "skipped", "neutral":
					default:
						anyFailed = true
					}
					continue
				}
				updated, err := fetchRunByID(ctx, client, owner, repo, inProgress[i].ID, token)
				if err != nil {
					allDone = false
					continue
				}
				inProgress[i] = updated
				if updated.Status != "completed" {
					allDone = false
					continue
				}
				switch updated.Conclusion {
				case "success", "skipped", "neutral":
				default:
					anyFailed = true
					notify(repoName, fmt.Sprintf("FAILED: %s -- %s", updated.Name, updated.HTMLURL))
				}
			}
			if allDone {
				if !anyFailed {
					notify(repoName, "CI passed")
				}
				if anyFailed {
					return 2
				}
				return 0
			}
		}
	}

	notify(repoName, fmt.Sprintf("watching %d workflow(s)...", len(runs)))

	for {
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(pollInterval):
		}

		runs, err := fetchRuns(ctx, client, owner, repo, sha, token)
		if err != nil {
			if oneshot {
				notify(repoName, "workflow check failed: "+err.Error())
				return 1
			}
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
				notify(repoName, fmt.Sprintf("FAILED: %s -- %s", r.Name, r.HTMLURL))
			}
		}

		if allDone {
			if !anyFailed {
				notify(repoName, "CI passed")
			}
			if anyFailed {
				return 2
			}
			return 0
		}
	}
}

func fetchRecentRuns(ctx context.Context, client *http.Client, owner, repo, token string) ([]workflowRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs?per_page=50", owner, repo)
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
	// API returns newest first; keep first occurrence of each workflow name.
	seen := make(map[string]bool)
	var latest []workflowRun
	for _, r := range result.WorkflowRuns {
		if !seen[r.Name] {
			seen[r.Name] = true
			latest = append(latest, r)
		}
	}
	return latest, nil
}

func fetchRunByID(ctx context.Context, client *http.Client, owner, repo string, id int64, token string) (workflowRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runs/%d", owner, repo, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return workflowRun{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return workflowRun{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return workflowRun{}, fmt.Errorf("GitHub API: %s", resp.Status)
	}

	var r workflowRun
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return workflowRun{}, err
	}
	return r, nil
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
