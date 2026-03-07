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

package workflow

import (
	"errors"
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	registeredWorkflows = map[string]Workflow{}
	registeredTasks     = map[string]Task{}
)

// LoadWorkflowsFromYAML loads workflows from a YAML file and registers them
func LoadWorkflowsFromYAML(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := yaml.NewDecoder(file)
	var workflows []Workflow
	err = decoder.Decode(&workflows)
	if err != nil {
		return err
	}

	for i := range workflows {
		err = validatePhases(workflows[i].SetUpHost)
		if err != nil {
			return err
		}
		err = validatePhases(workflows[i].SetUpCluster)
		if err != nil {
			return err
		}
		err = validatePhases(workflows[i].TearDownHost)
		if err != nil {
			return err
		}
		err = validatePhases(workflows[i].TearDownCluster)
		if err != nil {
			return err
		}

		RegisterWorkflow(workflows[i])
	}

	return nil
}

func validatePhases(phases []Phase) error {
	var invalidTasks []string
	for _, phase := range phases {
		for _, task := range phase.Tasks {
			_, ok := registeredTasks[task]
			if !ok {
				invalidTasks = append(invalidTasks, task)
			}
		}
	}

	if len(invalidTasks) > 0 {
		return errors.New("invalid tasks: " + strings.Join(invalidTasks, ", "))
	}

	return nil
}

// NewTask creates a new instance of a registered task by name
func NewTask(name string) Task {
	task, ok := registeredTasks[name]
	if !ok {
		return nil
	}
	t := reflect.TypeOf(task)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	taskPtr := reflect.New(t)
	return taskPtr.Interface().(Task)
}

// RegisterTask registers a task type by name
func RegisterTask(task Task) {
	if task == nil {
		return
	}

	t := reflect.TypeOf(task)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	name := t.Name()
	if name == "" {
		return
	}

	registeredTasks[name] = task
}

// RegisterWorkflow registers a workflow by name
func RegisterWorkflow(wf Workflow) {
	registeredWorkflows[wf.Name] = wf
}
