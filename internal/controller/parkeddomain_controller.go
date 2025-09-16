/*
Copyright 2025.

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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	parkingv1alpha1 "github.com/gminiba/parked-domain-operator/api/v1alpha1"
)

const finalizerName = "parking.minibaev.eu/finalizer"

// ParkedDomainReconciler reconciles a ParkedDomain object
type ParkedDomainReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	S3Client  *s3.Client
	R53Client *route53.Client
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=parking.minibaev.eu,resources=parkeddomains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=parking.minibaev.eu,resources=parkeddomains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=parking.minibaev.eu,resources=parkeddomains/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ParkedDomainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the ParkedDomain instance
	pd := &parkingv1alpha1.ParkedDomain{}
	err := r.Get(ctx, req.NamespacedName, pd)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ParkedDomain resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ParkedDomain")
		return ctrl.Result{}, err
	}

	// 2. Handle Finalizer for cleanup
	if pd.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so we add our finalizer if it doesn't exist.
		if !controllerutil.ContainsFinalizer(pd, finalizerName) {
			controllerutil.AddFinalizer(pd, finalizerName)
			if err := r.Update(ctx, pd); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted.
		if controllerutil.ContainsFinalizer(pd, finalizerName) {
			logger.Info("Performing cleanup for ParkedDomain")

			if err := r.cleanupS3Bucket(ctx, pd); err != nil {
				logger.Error(err, "S3 cleanup failed")
				return ctrl.Result{}, err
			}

			if err := r.cleanupRoute53Zone(ctx, pd); err != nil {
				logger.Error(err, "Route53 cleanup failed")
				return ctrl.Result{}, err
			}

			// All cleanup successful, remove the finalizer.
			controllerutil.RemoveFinalizer(pd, finalizerName)
			if err := r.Update(ctx, pd); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// 3. Reconcile AWS Resources by calling helper functions
	logger.Info("Reconciling AWS resources")

	zoneID, nameservers, err := r.reconcileRoute53Zone(ctx, pd)
	if err != nil {
		pd.Status.Status = "Error: Route53 Zone"
		_ = r.Status().Update(ctx, pd)
		return ctrl.Result{}, err
	}

	s3Endpoint, err := r.reconcileS3Bucket(ctx, pd)
	if err != nil {
		pd.Status.Status = "Error: S3 Bucket"
		_ = r.Status().Update(ctx, pd)
		return ctrl.Result{}, err
	}

	err = r.reconcileRoute53ARecord(ctx, pd, zoneID, s3Endpoint)
	if err != nil {
		pd.Status.Status = "Error: Route53 A Record"
		_ = r.Status().Update(ctx, pd)
		return ctrl.Result{}, err
	}

	// 4. Update the Status of the CR
	pd.Status.Status = "Provisioned"
	pd.Status.ZoneID = zoneID
	pd.Status.NameServers = nameservers
	if err := r.Status().Update(ctx, pd); err != nil {
		logger.Error(err, "Failed to update ParkedDomain status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled ParkedDomain")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ParkedDomainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Note: Initialize AWS clients here or in main.go to avoid re-creating them on every reconcile.
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return err
	}
	r.S3Client = s3.NewFromConfig(cfg)
	r.R53Client = route53.NewFromConfig(cfg)

	return ctrl.NewControllerManagedBy(mgr).
		For(&parkingv1alpha1.ParkedDomain{}).
		Complete(r)
}
