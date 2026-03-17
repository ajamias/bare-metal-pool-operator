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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BareMetalPool condition types
const (
	BareMetalPoolConditionTypeReady      = "Ready"
	BareMetalPoolConditionTypeHostsReady = "HostsReady"
)

// BareMetalPool condition reasons for Ready condition
const (
	// BareMetalPoolReasonReady indicates the pool is fully ready
	BareMetalPoolReasonReady = "Ready"

	// BareMetalPoolReasonProgressing indicates the pool is being processed
	BareMetalPoolReasonProgressing = "Progressing"

	// BareMetalPoolReasonFailed indicates the pool has failed
	BareMetalPoolReasonFailed = "Failed"

	// BareMetalPoolReasonDeleting indicates the pool is being deleted
	BareMetalPoolReasonDeleting = "Deleting"
)

// BareMetalPool condition reasons for HostsReady condition
const (
	// BareMetalPoolReasonHostsAvailable indicates all required Host CRs have been created
	BareMetalPoolReasonHostsAvailable = "HostsAvailable"

	// BareMetalPoolReasonHostsProgressing indicates Host CRs are being created or deleted
	BareMetalPoolReasonHostsProgressing = "HostsProgressing"

	// BareMetalPoolReasonHostsDeleting indicates Host CRs are being deleted
	BareMetalPoolReasonHostsDeleting = "HostsDeleting"
)

// InitializeStatusConditions initializes the BareMetalPool conditions
func (c *BareMetalPool) InitializeStatusConditions() {
	c.initializeStatusCondition(
		BareMetalPoolConditionTypeReady,
		metav1.ConditionFalse,
		BareMetalPoolReasonProgressing,
	)
	c.initializeStatusCondition(
		BareMetalPoolConditionTypeHostsReady,
		metav1.ConditionFalse,
		BareMetalPoolReasonHostsProgressing,
	)
}

func (c *BareMetalPool) SetStatusCondition(
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
) {
	meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// initializeStatusCondition initializes a single condition if it doesn't already exist
func (c *BareMetalPool) initializeStatusCondition(
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
) {
	if c.Status.Conditions == nil {
		c.Status.Conditions = []metav1.Condition{}
	}

	// If condition already exists, don't overwrite
	if meta.FindStatusCondition(c.Status.Conditions, conditionType) != nil {
		return
	}

	c.SetStatusCondition(conditionType, status, reason, "Initialized")
}
