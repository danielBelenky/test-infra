package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/signal"

	"flag"
	"os"

	log "github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/external-plugins/experiment/pkg/handler"
	"k8s.io/test-infra/prow/github"
)

type options struct {
	ProwConfigPath         string
	JobConfigPathsPatterns string
	Addr                   string
	WorkDir 			   string
	// dryRun bool
}

// Validate the options
func (opts *options) Validate() {
	var validationErrors []error
	if opts.ProwConfigPath == "" {
		validationErrors = append(validationErrors, errors.New("prow config path was not specified"))
	}
	if opts.JobConfigPathsPatterns == "" {
		validationErrors = append(validationErrors, errors.New("job config path patterns were not specified"))
	}

	if len(validationErrors) == 0 {
		return
	}
	for _, err := range validationErrors {
		log.Errorln(err.Error())
	}
	os.Exit(1)
}

func gatherOptions() options {
	opts := options{}
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&opts.ProwConfigPath, "config-path", "", "Path to config.yaml")
	fs.StringVar(&opts.JobConfigPathsPatterns,
		"job-config-patterns", "",
		"Comma separated shell filename patterns for prowjob configs.",
	)
	fs.StringVar(&opts.Addr, "address", ":8720",
		"ip:port to listen on. Defaults to localhost:8720",
	)
	err := fs.Parse(os.Args[1:])
	if err != nil {
		log.Errorln(err.Error())
		os.Exit(1)
	}
	return opts
}

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
}

func eventsHandler(opts options) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		var event github.PullRequestEvent
		err := json.NewDecoder(req.Body).Decode(&event)
		if err != nil {
			log.Errorf("Error handling event: %s", err.Error())
			return
		}
		go handler.HandlePullRequestEvent(&event, opts.ProwConfigPath, opts.JobConfigPathsPatterns, opts.WorkDir)
	})
}

func shutdown(c <-chan os.Signal, s *http.Server) {
	select {
	case <-c:
		log.Infoln("Shutting down...")
		s.Shutdown(context.Background())
	}

}

func main() {
	opts := gatherOptions()
	opts.Validate()
	router := http.NewServeMux()
	router.Handle("/", eventsHandler(opts))

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)

	server := &http.Server{
		Addr:	 opts.Addr,
		Handler: router,
	}
	log.Infof("Listening for events on: %s", opts.Addr)
	go shutdown(c, server)
	server.ListenAndServe()
}
