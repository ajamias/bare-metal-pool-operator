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

package controller

import (
	"context"
	"fmt"
	"sort"

	hostv1alpha1 "github.com/DanNiESh/host-operator/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ajamias/bare-metal-operator/api/v1alpha1"
	"github.com/ajamias/bare-metal-operator/internal/profile"
)

// BareMetalPoolReconciler reconciles a BareMetalPool object
type BareMetalPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const BareMetalPoolFinalizer = "osac.openshift.io/bare-metal-pool"

// +kubebuilder:rbac:groups=osac.openshift.io,resources=baremetalpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=osac.openshift.io,resources=baremetalpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=osac.openshift.io,resources=baremetalpools/finalizers,verbs=update
// +kubebuilder:rbac:groups=osac.openshift.io,resources=hosts,verbs=get;list;watch;create;delete;deletecollection
// +kubebuilder:rbac:groups=tekton.dev,resources=pipelineruns,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the pool closer to the desired state.
func (r *BareMetalPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	bareMetalPool := &v1alpha1.BareMetalPool{}
	err := r.Get(ctx, req.NamespacedName, bareMetalPool)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Info(
		"Starting reconcile",
		"namespacedName",
		req.NamespacedName,
		"name",
		bareMetalPool.Name,
	)

	oldstatus := bareMetalPool.Status.DeepCopy()

	var result ctrl.Result
	if !bareMetalPool.DeletionTimestamp.IsZero() {
		err = r.handleDeletion(ctx, bareMetalPool)
		result = ctrl.Result{}
	} else {
		result, err = r.handleUpdate(ctx, bareMetalPool)
	}

	if !equality.Semantic.DeepEqual(bareMetalPool.Status, *oldstatus) {
		status := metav1.ConditionFalse
		hostsReason := meta.FindStatusCondition(
			bareMetalPool.Status.Conditions,
			v1alpha1.BareMetalPoolConditionTypeHostsReady,
		).Reason

		var reason string
		switch hostsReason {
		case v1alpha1.BareMetalPoolReasonHostsAvailable:
			status = metav1.ConditionTrue
			reason = v1alpha1.BareMetalPoolReasonReady
		case v1alpha1.BareMetalPoolReasonHostsProgressing:
			reason = v1alpha1.BareMetalPoolReasonProgressing
		case v1alpha1.BareMetalPoolReasonHostsDeleting:
			reason = v1alpha1.BareMetalPoolReasonDeleting
		default:
			reason = v1alpha1.BareMetalPoolReasonFailed
		}

		bareMetalPool.SetStatusCondition(
			v1alpha1.BareMetalPoolConditionTypeReady,
			status,
			reason,
			hostsReason,
		)

		statusErr := r.Status().Update(ctx, bareMetalPool)
		if statusErr != nil {
			return result, statusErr
		}
	}

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *BareMetalPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.BareMetalPool{}).
		Named("baremetalpool").
		Complete(r)
}

// handleUpdate processes BareMetalPool creation or specification updates.
func (r *BareMetalPoolReconciler) handleUpdate(ctx context.Context, bareMetalPool *v1alpha1.BareMetalPool) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Handling update", "name", bareMetalPool.Name)

	bareMetalPool.InitializeStatusConditions()

	if controllerutil.AddFinalizer(bareMetalPool, BareMetalPoolFinalizer) {
		err := r.Update(ctx, bareMetalPool)
		return ctrl.Result{}, err
	}

	if bareMetalPool.Status.HostSets == nil {
		bareMetalPool.Status.HostSets = map[string]int{}
	}

	// List all Host CRs owned by this BareMetalPool
	hostList := &hostv1alpha1.HostList{}
	err := r.List(ctx, hostList,
		client.InNamespace(bareMetalPool.Namespace),
		client.MatchingLabels{"osac.openshift.io/pool-id": string(bareMetalPool.UID)},
	)
	if err != nil {
		log.Error(err, "Failed to list Host CRs")
		return ctrl.Result{}, err
	}

	// Group current hosts per hostClass, sorted by name for ordinal consistency
	currentReplicas := map[string]int{}
	currentHosts := map[string][]*hostv1alpha1.Host{}
	for i := range hostList.Items {
		hostClass := hostList.Items[i].Spec.Matches.HostClass
		currentReplicas[hostClass]++
		currentHosts[hostClass] = append(currentHosts[hostClass], &hostList.Items[i])
	}
	for hostClass := range currentHosts {
		sort.Slice(currentHosts[hostClass], func(i, j int) bool {
			// TODO: doesnt work for replicas > 9, need to convert ordinal to int first
			return currentHosts[hostClass][i].Name < currentHosts[hostClass][j].Name
		})
	}

	var registeredProfile *profile.Profile
	if pf, ok := profile.Get(bareMetalPool.Spec.Profile.Name); ok {
		registeredProfile = &pf
	}

	// Scale up or down for each desired hostClass
	scaled := false
	defer r.updateStatusHostSets(bareMetalPool, currentReplicas)
	for hostClass, replicas := range bareMetalPool.Spec.HostSets {
		delta := replicas - currentReplicas[hostClass]
		if delta > 0 {
			scaled = true
			for i := 0; i < delta; i++ {
				ordinal := currentReplicas[hostClass]
				if err := r.createHostCR(ctx, bareMetalPool, hostClass, ordinal, registeredProfile); err != nil {
					log.Error(err, "Failed to create Host CR", "hostClass", hostClass)
					return ctrl.Result{}, err
				}
				currentReplicas[hostClass]++
			}
		} else if delta < 0 {
			scaled = true
			hostsToDelete := currentHosts[hostClass][replicas:]
			for i := range hostsToDelete {
				if err := r.Delete(ctx, hostsToDelete[i]); client.IgnoreNotFound(err) != nil {
					log.Error(err, "Failed to delete Host CR", "host", hostsToDelete[i].Name)
					return ctrl.Result{}, err
				}
				currentReplicas[hostClass]--
			}
		}
	}

	// Delete hosts for hostClasses no longer in spec
	for hostClass, hosts := range currentHosts {
		if _, ok := bareMetalPool.Spec.HostSets[hostClass]; ok {
			continue
		}
		scaled = true
		for i := range hosts {
			if err := r.Delete(ctx, hosts[i]); client.IgnoreNotFound(err) != nil {
				log.Error(err, "Failed to delete Host CR", "host", hosts[i].Name)
				return ctrl.Result{}, err
			}
			currentReplicas[hostClass]--
		}
	}

	bareMetalPool.SetStatusCondition(
		v1alpha1.BareMetalPoolConditionTypeHostsReady,
		metav1.ConditionTrue,
		v1alpha1.BareMetalPoolReasonHostsAvailable,
		"Successfully reconciled hosts",
	)

	if registeredProfile != nil && scaled {
		err = r.runWorkflow(ctx, bareMetalPool, registeredProfile.ClusterSetupWorkflow)
		if err != nil {
			log.Error(err, "Failed to set up pool")
			bareMetalPool.SetStatusCondition(
				v1alpha1.BareMetalPoolConditionTypeReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalPoolReasonFailed,
				"Failed to set up pool",
			)
			return ctrl.Result{}, err
		}
	}

	log.Info("Successfully reconciled")

	return ctrl.Result{}, nil
}

// handleDeletion handles the cleanup when a BareMetalPool is being deleted
func (r *BareMetalPoolReconciler) handleDeletion(ctx context.Context, bareMetalPool *v1alpha1.BareMetalPool) error {
	log := logf.FromContext(ctx)
	log.Info("Handling delete", "name", bareMetalPool.Name)

	bareMetalPool.SetStatusCondition(
		v1alpha1.BareMetalPoolConditionTypeReady,
		metav1.ConditionFalse,
		v1alpha1.BareMetalPoolReasonDeleting,
		"BareMetalPool is being torn down",
	)

	bareMetalPool.SetStatusCondition(
		v1alpha1.BareMetalPoolConditionTypeHostsReady,
		metav1.ConditionFalse,
		v1alpha1.BareMetalPoolReasonHostsDeleting,
		"BareMetalPool's hosts are being torn down",
	)

	registeredProfile, ok := profile.Get(bareMetalPool.Spec.Profile.Name)
	if ok {
		err := r.runWorkflow(ctx, bareMetalPool, registeredProfile.ClusterTeardownWorkflow)
		if err != nil {
			log.Error(err, "Failed to tear down pool")
			return err
		}
	}

	// Owner references will cascade-delete the Host CRs.
	// The host-inventory-operator's finalizer on each Host CR ensures
	// inventory hosts are freed before the Host CRs are fully removed.

	log.Info("Successfully deleted pool")
	if controllerutil.RemoveFinalizer(bareMetalPool, BareMetalPoolFinalizer) {
		err := r.Update(ctx, bareMetalPool)
		return err
	}

	return nil
}

// createHostCR creates a new Host CR owned by this BareMetalPool
func (r *BareMetalPoolReconciler) createHostCR(
	ctx context.Context,
	bareMetalPool *v1alpha1.BareMetalPool,
	hostClass string,
	ordinal int,
	registeredProfile *profile.Profile,
) error {
	log := logf.FromContext(ctx)

	hostName := fmt.Sprintf("%s-host-%s-%d", bareMetalPool.Name, hostClass, ordinal)
	namespace := bareMetalPool.Namespace
	labels := map[string]string{
		"osac.openshift.io/pool-id":    string(bareMetalPool.UID),
		"osac.openshift.io/host-class": hostClass,
	}

	var (
		managedBy        string
		provisionState   string
		setupWorkflow    string
		teardownWorkflow string
		workflowInput    map[string]string
	)
	if registeredProfile != nil {
		managedBy = registeredProfile.MatchHosts.ManagedBy
		provisionState = registeredProfile.MatchHosts.ProvisionState
		setupWorkflow = registeredProfile.HostSetupWorkflow.String()
		teardownWorkflow = registeredProfile.HostTeardownWorkflow.String()
		workflowInput = bareMetalPool.Spec.Profile.Input
	}

	hostCR := &hostv1alpha1.Host{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: hostv1alpha1.HostSpec{
			Matches: hostv1alpha1.MatchExpressions{
				HostClass:      hostClass,
				ManagedBy:      managedBy,
				ProvisionState: provisionState,
			},
			SetUpWorkflow: &hostv1alpha1.WorkflowSpec{
				WorkflowID: setupWorkflow,
				Input:      workflowInput,
			},
			TearDownWorkflow: &hostv1alpha1.WorkflowSpec{
				WorkflowID: teardownWorkflow,
			},
		},
	}
	if err := controllerutil.SetControllerReference(bareMetalPool, hostCR, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference", "host", hostName)
		return err
	}
	if err := r.Create(ctx, hostCR); client.IgnoreAlreadyExists(err) != nil {
		log.Error(err, "Failed to create Host CR", "host", hostName)
		return err
	}

	log.Info("Created Host CR", "host", hostName)
	return nil
}

// updateStatusHostSets updates status.HostSets from the current replica counts.
func (r *BareMetalPoolReconciler) updateStatusHostSets(bareMetalPool *v1alpha1.BareMetalPool, currentReplicas map[string]int) {
	updatedHostSets := make(map[string]int)
	for hostClass, replicas := range currentReplicas {
		if replicas > 0 {
			updatedHostSets[hostClass] = replicas
		}
	}
	bareMetalPool.Status.HostSets = updatedHostSets
}

// runWorkflow runs a workflow by creating a Tekton PipelineRun
func (r *BareMetalPoolReconciler) runWorkflow(ctx context.Context, bareMetalPool *v1alpha1.BareMetalPool, workflowReference profile.WorkflowReference) error {
	log := logf.FromContext(ctx)
	pfName := bareMetalPool.Spec.Profile.Name

	if workflowReference.String() == "" {
		log.Info("Skipping workflow")
		return nil
	}

	// Convert map[string]string input to Tekton params
	params := make([]tektonv1.Param, 0, len(bareMetalPool.Spec.Profile.Input))
	for k, v := range bareMetalPool.Spec.Profile.Input {
		params = append(params, tektonv1.Param{
			Name:  k,
			Value: tektonv1.ParamValue{Type: tektonv1.ParamTypeString, StringVal: v},
		})
	}

	// Create PipelineRun
	pipelineRunName := fmt.Sprintf("%s-%s", workflowReference.String(), rand.String(8))
	pipelineRun := &tektonv1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipelineRunName,
			Namespace: bareMetalPool.Namespace,
			Labels: map[string]string{
				"osac.openshift.io/pool-id": string(bareMetalPool.UID),
				"osac.openshift.io/profile": pfName,
			},
		},
		Spec: tektonv1.PipelineRunSpec{
			PipelineRef: &workflowReference.WorkflowRef,
			Params:      params,
		},
	}

	// Set owner reference so PipelineRun is deleted when BareMetalPool is deleted
	err := controllerutil.SetControllerReference(bareMetalPool, pipelineRun, r.Scheme)
	if err != nil {
		return err
	}

	// Create the PipelineRun
	err = r.Create(ctx, pipelineRun)
	if err != nil {
		return err
	}

	log.Info("Created PipelineRun", "name", pipelineRunName)

	return nil
}
