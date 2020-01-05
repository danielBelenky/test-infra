package git_utils

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/github"
)

const (
	PULL_REQUEST_HEAD = "prhead"
	PULL_REQUEST_BASE = "prbase"
)

// Git is a utility function to execute git commands
// It returns the captured output of the command and and error if it occurs..
func Git(args ...string) ([]byte, error) {
	// cmd := exec.Command("git", append(args[0:], "--progress", "--no-pager")...)
	cmd := exec.Command("git", args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, logAndReturn(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, logAndReturn(err)
	}
	log.Infof("Executing git command: %s", cmd.String())
	err = cmd.Start()
	if err != nil {
		return nil, logAndReturn(err)
	}
	go logProgress(stderr)
	stdoutBytes, err := ioutil.ReadAll(stdout)
	if err != nil {
		return nil, logAndReturn(err)
	}
	err = cmd.Wait()

	if err != nil {
		return nil, logAndReturn(err)
	}

	ec := cmd.ProcessState.ExitCode()
	log.Info("Git process exited with: ", ec)
	if ec != 0 {
		return nil, fmt.Errorf("git process exited with %d", ec)
	}
	return stdoutBytes, nil
}

// fetchPullRequest fetches the head and the base ref of the given PR locally,
// giving random UUID for each of them.
func FetchPullRequest(repo string, pr *github.PullRequest) error {
	_, err := Git(
		"-C", repo, "fetch", "--depth=50", pr.Base.Repo.HTMLURL,
		fmt.Sprintf("+pull/%d/head:%s", pr.Number, PULL_REQUEST_HEAD),
		fmt.Sprintf("+%s:%s", pr.Base.Ref, PULL_REQUEST_BASE))
	if err != nil {
		return err
	}

	return nil
}

// ChangedFiles returns the diff `to` and the merge base with `from`.
// A filter can be provided to select which files are being returned.
// Empty filter means everything.
func ChangedFiles(repo, from, to, filter, diffFilter string) ([]string, error) {
	out, err := Git(
		"-C", repo, "diff", "--name-only",
		fmt.Sprintf("--diff-filter=%s", filter),
		fmt.Sprintf("%s...%s", from, to),
		diffFilter,
	)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func logAndReturn(err error) error {
	log.Errorln(err.Error())
	return err
}

// MergeBase returns the merge base between refA and refB
func MergeBase(repo, refA, refB string) (string, error) {
	out, err := Git("-C", repo, "merge-base", refA, refB)
	if err != nil {
		return "", err
	}
	return strings.Fields(string(out))[0], nil
}

// WorktreeAdd adds a worktree at the given dest with the specified ref
func WorktreeAdd(repo, ref, dest string) error {
	_, err := Git("-C", repo, "worktree", "add", dest, ref)
	return err
}

// InitRepo initializes a bare git repository at the given path.
func InitRepo(path string, bare bool) error {
	cmd := []string{"init"}
	if bare == true {
		cmd = append(cmd, "--bare")
	}
	cmd = append(cmd, path)
	_, err := Git(cmd...)
	return err
}

func logProgress(rc io.ReadCloser) {
	stderrReader := bufio.NewScanner(rc)
	for stderrReader.Scan() {
		log.Infoln(stderrReader.Text())
	}
}
