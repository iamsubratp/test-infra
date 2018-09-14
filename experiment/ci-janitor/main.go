/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// ci-janitor cleans up dedicated projects in k8s prowjob configs
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/test-infra/prow/config"
)

type options struct {
	configPath    string
	jobConfigPath string
	janitorPath   string
}

var (
	defaultTTL = 24
	soakTTL    = 24 * 10
)

func (o *options) Validate() error {
	if o.configPath == "" {
		return errors.New("required flag --config-path was unset")
	}

	if o.jobConfigPath == "" {
		return errors.New("required flag --job-config-path was unset")
	}

	if o.janitorPath == "" {
		return errors.New("required flag --janitor-path was unset")
	}

	return nil
}

func gatherOptions() options {
	o := options{}
	flag.StringVar(&o.configPath, "config-path", "", "Path to config.yaml.")
	flag.StringVar(&o.jobConfigPath, "job-config-path", "", "Path to prow job configs.")
	flag.StringVar(&o.janitorPath, "janitor-path", "", "Path to janitor.py.")
	flag.Parse()
	return o
}

func findProject(spec *v1.PodSpec) (string, int) {
	project := ""
	ttl := defaultTTL
	for _, container := range spec.Containers {
		for _, arg := range container.Args {
			if strings.HasPrefix(arg, "--gcp-project=") {
				project = strings.TrimPrefix(arg, "--gcp-project=")
			}

			if arg == "--soak" {
				ttl = soakTTL
			}
		}
	}

	return project, ttl
}

func clean(proj, janitorPath string, ttl int) error {
	logrus.Infof("Will clean up %s with ttl %d h", proj, ttl)

	cmd := exec.Command(janitorPath, fmt.Sprintf("--project=%s", proj), fmt.Sprintf("--hour=%d", ttl))
	b, err := cmd.CombinedOutput()
	if err != nil {
		logrus.WithError(err).Errorf("failed to clean up project %s, error info: %s", proj, string(b))
	} else {
		logrus.Infof("successfully cleaned up project %s", proj)
	}

	return err
}

func main() {
	o := gatherOptions()
	if err := o.Validate(); err != nil {
		logrus.Fatalf("Invalid options: %v", err)
	}

	conf, err := config.Load(o.configPath, o.jobConfigPath)
	if err != nil {
		logrus.WithError(err).Fatal("Error loading config.")
	}

	failed := []string{}

	for _, v := range conf.AllPresubmits(nil) {
		if project, ttl := findProject(v.Spec); project != "" {
			if err := clean(project, o.janitorPath, ttl); err != nil {
				failed = append(failed, project)
			}
		}
	}

	for _, v := range conf.AllPostsubmits(nil) {
		if project, ttl := findProject(v.Spec); project != "" {
			if err := clean(project, o.janitorPath, ttl); err != nil {
				failed = append(failed, project)
			}
		}
	}

	for _, v := range conf.AllPeriodics() {
		if project, ttl := findProject(v.Spec); project != "" {
			if err := clean(project, o.janitorPath, ttl); err != nil {
				failed = append(failed, project)
			}
		}
	}

	if len(failed) == 0 {
		logrus.Info("Successfully cleaned up all projects!")
		os.Exit(0)
	}

	logrus.Warnf("Failed clean %d projects: %v", len(failed), failed)
	os.Exit(1)
}