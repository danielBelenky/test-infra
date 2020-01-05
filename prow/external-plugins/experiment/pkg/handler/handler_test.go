package handler

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"path/filepath"
	"testing"

	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/github"
)

const (
	resourceDir = "test_resources"
)

func loadTestResource(t *testing.T, res string) []byte {
	b, err := ioutil.ReadFile(filepath.Join(resourceDir, res))
	assert.NotNil(t, err, err.Error())
	return b
}

func loadPullRequestResource(t *testing.T, name string) *github.PullRequestEvent {
	b := loadTestResource(t, name)
	var event github.PullRequestEvent
	err := json.Unmarshal(b, &event)
	assert.NotNil(t, err)
	return &event
}

func loadConfigResource(t *testing.T, name string) *config.Config {
	prowConfigPath := filepath.Join(resourceDir, "config.yaml")
	jobConfigPath := filepath.Join(resourceDir, name)
	conf, err := config.Load(prowConfigPath, jobConfigPath)
	assert.Nil(t, err)
	return conf
}

func TestSquashConfigs(t *testing.T) {
	originalConfigs := configPath{
		"foo/path": &config.Config{
			JobConfig:  config.JobConfig{
				PresubmitsStatic: map[string][]config.Presubmit{
					"foo/bar": {
						{
							JobBase: config.JobBase{
								Name: "dont-touch",
							},
						},
						{
							JobBase: config.JobBase{
								Name: "modify-something",
								MaxConcurrency: 1,
							},
						},
					},
					"foo/baz": {
						{
							JobBase: config.JobBase{
								Name: "dont-touch",
							},
						},
					},
				},
			},
		},
	}
	modifiedConfigs := configPath{
		"foo/path": &config.Config{
			JobConfig:  config.JobConfig{
				PresubmitsStatic: map[string][]config.Presubmit{
					"foo/bar": {
						{
							JobBase: config.JobBase{
								Name: "dont-touch",
							},
						},
						{
							JobBase: config.JobBase{
								Name: "modify-something",
								MaxConcurrency: 2,
							},
						},
					},
					"foo/baz": {
						{
							JobBase: config.JobBase{
								Name: "dont-touch",
							},
						},
						{
							JobBase: config.JobBase{
								Name: "new-presubmit",
								MaxConcurrency: 1,
							},
						},
					},
				},
			},
		},
	}
	expectedConfigs := []*config.Config{
		{
			JobConfig:  config.JobConfig{
				PresubmitsStatic: map[string][]config.Presubmit{
					"foo/bar": {
						{
							JobBase: config.JobBase{
								Name: "modify-something",
								MaxConcurrency: 2,
							},
						},
					},
					"foo/baz": {
						{
							JobBase: config.JobBase{
								Name: "new-presubmit",
								MaxConcurrency: 1,
							},
						},
					},
				},
			},
		},
	}
	res := squashConfigs(originalConfigs, modifiedConfigs)
	assert.Equal(t, expectedConfigs, res)
}

func TestSquashPresubmitConfigs(t *testing.T) {
	originalConfigs := map[string][]config.Presubmit {
		"foo/bar": {
			{
				JobBase: config.JobBase{
					Name: "dont-touch",
				},
			},
			{
				JobBase: config.JobBase{
					Name: "modify-something",
					MaxConcurrency: 1,
				},
			},
		},
		"foo/baz": {
			{
				JobBase: config.JobBase{
					Name: "dont-touch",
				},
			},
		},
	}
	modifiedConfigs := map[string][]config.Presubmit {
		"foo/bar": {
			{
				JobBase: config.JobBase{
					Name: "dont-touch",
				},
			},
			{
				JobBase: config.JobBase{
					Name: "modify-something",
					// Modified MaxConcurrency
					MaxConcurrency: 2,
				},
			},
		},
		"foo/baz": {
			{
				JobBase: config.JobBase{
					Name: "dont-touch",
				},
			},
			{
				JobBase: config.JobBase{
					Name: "new-presubmit",
				},
			},
		},
	}
	expectedConfigs := map[string][]config.Presubmit {
		"foo/bar": {
			{
				JobBase: config.JobBase{
					Name: "modify-something",
					// Modified MaxConcurrency
					MaxConcurrency: 2,
				},
			},
		},
		"foo/baz": {
			{
				JobBase: config.JobBase{
					Name: "new-presubmit",
				},
			},
		},
	}

	res := squashPresubmitsConfigs(originalConfigs, modifiedConfigs)
	assert.Equal(t, res, expectedConfigs)
}

func TestSquashPresubmits(t *testing.T) {
	originalPresubmits := []config.Presubmit{
		{
			JobBase:             config.JobBase{
				Name: "dont-touch",
			},
		},
		{
			JobBase:             config.JobBase{
				Name: "modify-something",
				MaxConcurrency: 1,
			},
		},
	}
	newPresubmits := []config.Presubmit{
		{
			JobBase:             config.JobBase{
				Name: "dont-touch",
			},
		},
		{
			JobBase:             config.JobBase{
				Name: "modify-something",
				// modified MaxConcurrency
				MaxConcurrency: 2,
			},
		},
		{
			JobBase:             config.JobBase{
				Name: "new-job",
			},
		},
	}
	expectedPresubmits := []config.Presubmit{
		{
			JobBase:             config.JobBase{
				Name: "modify-something",
				// modified MaxConcurrency
				MaxConcurrency: 2,
			},
		},
		{
			JobBase:             config.JobBase{
				Name: "new-job",
			},
		},
	}
	res := squashPresubmits(originalPresubmits, newPresubmits)
	assert.Equal(t, expectedPresubmits, res)
}