package handler

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sigs.k8s.io/yaml"
	"strings"

	"github.com/sirupsen/logrus"

	prowapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"k8s.io/test-infra/prow/config"
	git "k8s.io/test-infra/prow/external-plugins/experiment/pkg/git_utils"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/pjutil"
)

const (
	gitDirPrefix = "prowjob_experiment_"
)

// Map from a the path the config was found it to the config itself
type configPath map[string]*config.Config

// HandlePullRequestEvent handles new pull request events
func HandlePullRequestEvent(event *github.PullRequestEvent, prowConfigPath, jobConfigPatterns, wd string) {
	// Make sure that even handler does not crash the program
	defer logOnPanic()

	logrus.Infof("Handling PR %d.", event.PullRequest.Number)

	workDir, err := makeWorkingDirectory(wd)
	if err != nil {
		logrus.WithError(err).Fatal("could not initialize working directory")
		return
	}
	// TODO: Add caching for improved performance
	defer os.RemoveAll(workDir)

	err = git.InitRepo(workDir, true)
	if err != nil {
		logrus.WithError(err).Fatal("could not initialize git repo")
		return
	}

	err = git.FetchPullRequest(workDir, &event.PullRequest)
	if err != nil {
		logrus.WithError(err).Fatal("could not fetch pull request")
		return
	}

	mergeBase, err := git.MergeBase(workDir, git.PULL_REQUEST_BASE, git.PULL_REQUEST_HEAD)
	if err != nil {
		logrus.WithError(err).Fatal("could not find merge base for the pull request")
	}

	modifiedJobConfigs, err := git.ChangedFiles(workDir, mergeBase, git.PULL_REQUEST_HEAD, "M", jobConfigPatterns)
	if err != nil {
		logrus.WithError(err).Fatal("could not extract modified files")
		return
	}
	logrus.Debug("%s configs were modified", len(modifiedJobConfigs))

	newJobConfigs, err := git.ChangedFiles(workDir, mergeBase, git.PULL_REQUEST_HEAD, "A", jobConfigPatterns)
	if err != nil {
		logrus.WithError(err).Fatal("could not extract newly added files")
		return
	}
	logrus.Debug("%s configs were added", len(newJobConfigs))

	if len(modifiedJobConfigs)+len(newJobConfigs) == 0 {
		logrus.Infof("No job configs were modified or added in PR: %d", event.Number)
		return
	}

	headTreeRoot, err := makeWorkTreeForRef(workDir, git.PULL_REQUEST_HEAD)
	if err != nil {
		logrus.WithError(err).Fatal("could not create work tree for PR's HEAD")
		return
	}

	baseTreeRoot, err := makeWorkTreeForRef(workDir, mergeBase)
	if err != nil {
		logrus.WithError(err).Fatal("could not make work tree for PR's merge base")
		return
	}

	originalConfigs := loadConfigs(baseTreeRoot, prowConfigPath, modifiedJobConfigs)
	modifiedConfigs := loadConfigs(headTreeRoot, prowConfigPath, modifiedJobConfigs)
	allConfigs := squashConfigs(originalConfigs, modifiedConfigs)
	newConfigs := loadConfigs(headTreeRoot, prowConfigPath, newJobConfigs)
	for _, newConfig := range newConfigs {
		allConfigs = append(allConfigs, newConfig)
	}

	prowJobs := generateProwJobs(allConfigs, event)
	writeJobs(prowJobs)
}

// Squash original and modified configs at the same path, returning an array of configs
// that contain only new and modified job configs.
func squashConfigs(originalConfigs, modifiedConfigs configPath) []*config.Config {
	var configs []*config.Config
	for path, headConfig := range modifiedConfigs {
		baseConfig, exists := originalConfigs[path]
		if !exists {
			// new config
			configs = append(configs, headConfig)
			continue
		}
		squashedConfig := new(config.Config)
		squashedConfig.PresubmitsStatic = squashPresubmitsConfigs(baseConfig.PresubmitsStatic, headConfig.PresubmitsStatic)
		configs = append(configs, squashedConfig)
	}
	return configs
}

func squashPresubmitsConfigs(originalPresubmits, modifiedPresubmits map[string][]config.Presubmit) map[string][]config.Presubmit {
	squashedPresubmitConfigs := make(map[string][]config.Presubmit)
	for repo, headPresubmits := range modifiedPresubmits {
		basePresubmits, exists := originalPresubmits[repo]
		if !exists {
			// new presubmits
			squashedPresubmitConfigs[repo] = headPresubmits
			continue
		}
		squashedPresubmitConfigs[repo] = squashPresubmits(basePresubmits, headPresubmits)
	}
	return squashedPresubmitConfigs
}

// Given two arrays of presubmits, return a new array containing only the modified and new ones.
func squashPresubmits(originalPresubmits, modifiedPresubmits []config.Presubmit) []config.Presubmit {
	var squashedPresubmits []config.Presubmit
	for _, headPresubmit := range modifiedPresubmits {
		presubmitIsNew := true
		for _, basePresubmit := range originalPresubmits {
			if basePresubmit.Name != headPresubmit.Name {
				continue
			}
			presubmitIsNew = false
			if reflect.DeepEqual(headPresubmit.Spec, basePresubmit.Spec) {
				continue
			}
			squashedPresubmits = append(squashedPresubmits, headPresubmit)
		}
		if presubmitIsNew {
			squashedPresubmits = append(squashedPresubmits, headPresubmit)
		}
	}
	return squashedPresubmits
}

func loadConfigs(root, prowConfPath string, jobConfPaths []string) configPath {
	configPaths := make(configPath)
	for _, jobConfPath := range jobConfPaths {
		prowConfInRepo := filepath.Join(root, prowConfPath)
		jobConfInRepo := filepath.Join(root, jobConfPath)
		conf, err := config.Load(prowConfInRepo, jobConfInRepo)
		if err != nil {
			logrus.Errorf("Failed to load config at %s: %s", jobConfInRepo, err.Error())
			continue
		}
		configPaths[jobConfPath] = conf
		logrus.Infof("Loaded config: %s", jobConfPath)
	}
	return configPaths
}

func writeJobs(jobs []prowapi.ProwJob) {
	for _, job := range jobs {
		y, _ := yaml.Marshal(&job)
		filename := fmt.Sprintf("/tmp/%s.yaml", job.GetName())
		err := ioutil.WriteFile(filename, y, 0644)
		if err != nil {
			logrus.Errorln(err.Error())
		}
	}
}

// Create a new work tree for the local ref out of the root
func makeWorkTreeForRef(root, ref string) (string, error) {
	wd := filepath.Join(root, ref)

	err := git.WorktreeAdd(root, ref, wd)
	if err != nil {
		return "", err
	}
	return wd, nil
}

// Create and return a temp directory at the specified path or use the system's
// temp dir if a path was not explicitly set.
func makeWorkingDirectory(wd string) (string, error) {
	if wd == "" {
		wd = os.TempDir()
	}
	dir, err := ioutil.TempDir(wd, gitDirPrefix)
	if err != nil {
		return "", err
	}
	return dir, nil
}

// Generate the ProwJobs from the configs and the PR event.
func generateProwJobs(configs []*config.Config, pre *github.PullRequestEvent) []prowapi.ProwJob {
	var jobs []prowapi.ProwJob

	logrus.Infoln("Will process jobs from ", len(configs), "configs")
	for _, conf := range configs {
		jobs = append(jobs, generatePresubmits(conf, pre)...)
	}

	return jobs
}

func generatePresubmits(conf *config.Config, pre *github.PullRequestEvent) []prowapi.ProwJob {
	var jobs []prowapi.ProwJob

	for repo, presubmits := range conf.PresubmitsStatic {
		for _, presubmit := range presubmits {
			pj := pjutil.NewPresubmit(pre.PullRequest, pre.PullRequest.Base.SHA, presubmit, pre.GUID)
			addRepoRef(&pj, repo)
			logrus.Infof("Adding job: %s", pj.Name)
			jobs = append(jobs, pj)
		}
	}

	return jobs
}

// Add ref of the original repo which we want to check.
func addRepoRef(prowJob *prowapi.ProwJob, repo string) {
	// pj.Spec.Refs[0] is being notified by reportlib.
	shouldSetWorkDir := true
	for _, ref := range prowJob.Spec.ExtraRefs {
		// If a WorkDir was set on one
		if ref.WorkDir {
			shouldSetWorkDir = false
			break
		}
	}
	repoSplit := strings.Split(repo, "/")
	org, name := repoSplit[0], repoSplit[1]
	ref := prowapi.Refs{
		Org:        org,
		Repo:       name,
		RepoLink:   fmt.Sprintf("https://github.com/%s", repo),
		BaseRef:    "refs/heads/master",
		WorkDir:    shouldSetWorkDir,
		CloneDepth: 1,
	}
	prowJob.Spec.ExtraRefs = append(prowJob.Spec.ExtraRefs, ref)
}

func logOnPanic() {
	if r := recover(); r != nil {
		logrus.Errorln(r)
	}
}
