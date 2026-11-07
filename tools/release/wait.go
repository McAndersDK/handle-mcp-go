package main

import (
	"encoding/json"
	"fmt"
)

type ghRun struct {
	ID         int64  `json:"databaseId"`
	HeadSHA    string `json:"headSha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type ghJobs struct {
	Conclusion string `json:"conclusion"`
	Jobs       []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
	} `json:"jobs"`
}

// waitRun waits for the run of a workflow on a branch or tag at one commit,
// prints the result per job and returns an error unless it succeeded.
func (e *Env) waitRun(workflow, ref, sha string) error {
	var run *ghRun
	for i := 0; i < pollMax && run == nil; i++ {
		out, err := e.gh("run", "list", "--workflow", workflow, "--branch", ref,
			"--json", "databaseId,headSha,status,conclusion", "--limit", "20")
		if err != nil {
			return err
		}
		var runs []ghRun
		if err := json.Unmarshal([]byte(out), &runs); err != nil {
			return fmt.Errorf("cannot read gh run list: %w", err)
		}
		for j := range runs {
			if runs[j].HeadSHA == sha {
				run = &runs[j]
				break
			}
		}
		if run == nil {
			e.logf("waiting for the %s run on %s ...", workflow, ref)
			e.Sleep(pollEvery)
		}
	}
	if run == nil {
		return fmt.Errorf("no %s run found for %s at %s", workflow, ref, sha)
	}
	e.logf("%s run %d: %s", workflow, run.ID, run.Status)
	_, watchErr := e.gh("run", "watch", fmt.Sprint(run.ID), "--exit-status")
	out, err := e.gh("run", "view", fmt.Sprint(run.ID), "--json", "conclusion,jobs")
	if err != nil {
		return err
	}
	var jobs ghJobs
	if err := json.Unmarshal([]byte(out), &jobs); err != nil {
		return fmt.Errorf("cannot read gh run view: %w", err)
	}
	for _, j := range jobs.Jobs {
		e.logf("  job %s: %s", j.Name, j.Conclusion)
	}
	if jobs.Conclusion != "success" {
		return fmt.Errorf("the %s run %d ended as %q (watch: %v)", workflow, run.ID, jobs.Conclusion, watchErr)
	}
	e.logf("%s run %d: success", workflow, run.ID)
	return nil
}
