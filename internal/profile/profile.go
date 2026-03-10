/*
Copyright 2026.

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

package profile

import (
	"os"

	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"gopkg.in/yaml.v3"
)

var registeredProfiles = map[string]Profile{}

// NewProfilesFromYAML loads profiles from a YAML file and registers them
func NewProfilesFromYAML(path string) ([]Profile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := yaml.NewDecoder(file)
	profiles := []Profile{}
	err = decoder.Decode(&profiles)
	if err != nil {
		return nil, err
	}

	return profiles, nil
}

// RegisterProfile registers a profile by name
func Register(pf Profile) {
	registeredProfiles[pf.Name] = pf
}

// GetProfile retrieves a registered workflow by name
func Get(name string) (Profile, bool) {
	pf, ok := registeredProfiles[name]
	return pf, ok
}

// Profile represents host and cluster lifecycle workflows
type Profile struct {
	Name              string              `yaml:"name"`
	MatchHosts        map[string]string   `yaml:"matchHosts"`
	SetHostProperties map[string]string   `yaml:"setHostProperties"`
	ExpectedInput     tektonv1.ParamSpecs `yaml:"expectedInput"`

	HostSetUpWorkflow       WorkflowReference `yaml:"hostSetUp"`
	HostTearDownWorkflow    WorkflowReference `yaml:"hostTearDown"`
	ClusterSetUpWorkflow    WorkflowReference `yaml:"clusterSetUp"`
	ClusterTearDownWorkflow WorkflowReference `yaml:"clusterTearDown"`
}

type WorkflowReference struct {
	WorkflowRef tektonv1.PipelineRef `yaml:"workflowRef,omitempty"`
}

func (r *WorkflowReference) String() string {
	return r.WorkflowRef.Name
}
