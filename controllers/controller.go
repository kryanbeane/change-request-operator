/*
Copyright 2022.

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

package controllers

import (
	"context"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"
)

type DeploymentController struct {
	client.Client
	*runtime.Scheme
}

const (
	deploymentControllerName = "deployment-controller"
	deploymentName           = "change-requester"
	serviceAccountName       = "rbac-manager-default"
	namespace                = "rbac-term-paper"
)

var labels = map[string]string{"app": "change-requester"}

var _ reconcile.Reconciler = &DeploymentController{}

func (p DeploymentController) Add(mgr manager.Manager) error {
	// Create a new Controller
	c, err := controller.New(deploymentControllerName, mgr,
		controller.Options{Reconciler: &DeploymentController{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}})
	if err != nil {
		logrus.Errorf("failed to create pod controller: %v", err)
		return err
	}
	// Make sure a deployment is managing the pod containing the controller
	if deployment, err := p.operatorDeployment(); err != nil {
		if err := p.ensureDeployment(deployment); err != nil {
			return err
		}
	}

	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(
		metav1.LabelSelector{
			MatchLabels: map[string]string{
				"rbac-management-operator.com/example": "running",
			},
		},
	)
	if err != nil {
		logrus.Errorf("Error creating label selector predicate: %v", err)
		return err
	}

	// Add a watch to Deployments containing that label
	err = c.Watch(
		&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForObject{}, labelSelectorPredicate)
	if err != nil {
		logrus.Errorf("error creating watch for deployments: %v", err)
		return err
	}

	return nil
}

//+kubebuilder:rbac:groups=core,resources=deployment,verbs=get;list;watch;create;update;patch;delete

func (p DeploymentController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logrus.Infof("Reconciling deployment %s", request.NamespacedName)

	var deployment appsv1.Deployment
	if err := p.Get(ctx, request.NamespacedName, &deployment); err != nil {
		if errors.IsNotFound(err) {
			logrus.Infof("Deployment %s not found", request.NamespacedName)
			return reconcile.Result{}, nil
		}
		logrus.Errorf("Error getting deployment %s: %v", request.NamespacedName, err)
		return reconcile.Result{}, err
	}

	deployment.Labels["rbac-management-operator.com/example"] = "successfully-edited"
	if err := p.Update(ctx, &deployment); err != nil {
		logrus.Errorf("Error updating deployment %s: %v", request.NamespacedName, err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (p DeploymentController) operatorDeployment() (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{
						{
							Name:  "change-requester",
							Image: "kryanbeane/change-request-operator:latest",
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(deployment, deployment, p.Scheme); err != nil {
		return deployment, err
	}

	return deployment, nil
}

func (p DeploymentController) ensureDeployment(deployment *appsv1.Deployment) error {
	// See if deployment already exists and create if it doesn't
	found := &appsv1.Deployment{}
	err := p.Client.Get(context.TODO(), types.NamespacedName{
		Name:      deployment.Name,
		Namespace: namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		// Create the deployment
		err = p.Client.Create(context.TODO(), deployment)

		if err != nil {
			// Deployment failed
			return err
		} else {
			// Deployment was successful
			return nil
		}
	} else if err != nil {
		// Error that isn't due to the deployment not existing
		return err
	}

	return nil
}
