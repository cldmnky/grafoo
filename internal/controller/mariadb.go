package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

// MariaDB metrics
var (
	// MariaDBReconcilerDuration tracks the time spent reconciling MariaDB
	MariaDBReconcilerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mariadb_reconciler_duration_seconds",
			Help:    "Duration of the MariaDB reconciler",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"namespace", "name", "operation"},
	)
	// MariaDBReconcilerErrors counts errors during MariaDB reconciliation
	MariaDBReconcilerErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mariadb_reconciler_errors_total",
			Help: "Total number of errors in the MariaDB reconciler",
		},
		[]string{"namespace", "name", "resource", "operation"},
	)
)

func init() {
	// Register MariaDB metrics
	prometheus.MustRegister(MariaDBReconcilerDuration)
	prometheus.MustRegister(MariaDBReconcilerErrors)
}

// ReconcileMariaDB reconciles the MariaDB component
func (r *GrafanaReconciler) ReconcileMariaDB(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling MariaDB")

	// Start timing the reconciliation
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		operation := "reconcile"
		if instance.Spec.MariaDB == nil || !instance.Spec.MariaDB.Enabled {
			operation = "cleanup"
		}
		MariaDBReconcilerDuration.WithLabelValues(instance.Namespace, instance.Name, operation).Observe(duration)
	}()

	// Check if MariaDB is enabled
	if instance.Spec.MariaDB == nil || !instance.Spec.MariaDB.Enabled {
		logger.Info("MariaDB is disabled, cleaning up resources")
		return r.cleanupMariaDB(ctx, instance)
	}

	// Step 1: Create or update the secret
	if err := r.reconcileMariaDBSecret(ctx, instance); err != nil {
		return err
	}

	// Step 2: Create or update the service account
	if err := r.reconcileMariaDBServiceAccount(ctx, instance); err != nil {
		return err
	}

	// Step 3: Create or update the PVC
	if err := r.reconcileMariaDBPVC(ctx, instance); err != nil {
		return err
	}

	// Step 4: Create or update the service
	if err := r.reconcileMariaDBService(ctx, instance); err != nil {
		return err
	}

	// Step 5: Create or update the deployment
	if err := r.reconcileMariaDBDeployment(ctx, instance); err != nil {
		return err
	}

	logger.Info("MariaDB reconciliation completed successfully")
	return nil
}

// cleanupMariaDB removes all MariaDB resources when disabled
func (r *GrafanaReconciler) cleanupMariaDB(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	// Resources to clean up
	resources := []struct {
		name string
		obj  client.Object
	}{
		{"deployment", &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "mariadb"),
				Namespace: instance.Namespace,
			},
		}},
		{"service", &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "mariadb"),
				Namespace: instance.Namespace,
			},
		}},
		{"pvc", &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "mariadb"),
				Namespace: instance.Namespace,
			},
		}},
		{"secret", &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "mariadb"),
				Namespace: instance.Namespace,
			},
		}},
		{"serviceaccount", &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "mariadb"),
				Namespace: instance.Namespace,
			},
		}},
	}

	// Delete each resource
	for _, res := range resources {
		if err := r.deleteResourceIfExists(ctx, res.obj, instance, res.name); err != nil {
			return err
		}
	}

	logger.Info("MariaDB resources cleaned up successfully")
	return nil
}

// deleteResourceIfExists deletes a resource if it exists
func (r *GrafanaReconciler) deleteResourceIfExists(ctx context.Context, obj client.Object, instance *grafoov1alpha1.Grafana, resourceType string) error {
	logger := log.FromContext(ctx)

	// Check if the resource exists
	err := r.Client.Get(ctx, client.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Resource doesn't exist, nothing to delete
			return nil
		}
		// Error getting resource
		logger.Error(err, "Failed to get resource", "type", resourceType, "name", obj.GetName())
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			resourceType,
			"get",
		).Inc()
		return err
	}

	// Delete the resource
	logger.Info("Deleting resource", "type", resourceType, "name", obj.GetName())
	if err := r.Client.Delete(ctx, obj); err != nil {
		logger.Error(err, "Failed to delete resource", "type", resourceType, "name", obj.GetName())
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			resourceType,
			"delete",
		).Inc()
		return err
	}

	logger.Info("Successfully deleted resource", "type", resourceType, "name", obj.GetName())
	return nil
}

// reconcileMariaDBSecret creates or updates the MariaDB secret
func (r *GrafanaReconciler) reconcileMariaDBSecret(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	mariadbSecret := &corev1.Secret{}
	secretName := r.generateNameForComponent(instance, "mariadb")
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: instance.Namespace,
	}, mariadbSecret)

	// If secret doesn't exist, create it with new credentials
	if apierrors.IsNotFound(err) {
		logger.Info("Creating MariaDB secret")
		mariadbSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: instance.Namespace,
				Labels:    r.generateLabelsForComponent(instance, "mariadb"),
			},
			StringData: map[string]string{
				"database-name":          "grafana",
				"database-password":      uuid.New().String(),
				"database-root-password": uuid.New().String(),
				"database-user":          "grafana",
			},
		}
		if err := ctrl.SetControllerReference(instance, mariadbSecret, r.Scheme); err != nil {
			logger.Error(err, "Failed to set controller reference on secret")
			MariaDBReconcilerErrors.WithLabelValues(
				instance.Namespace,
				instance.Name,
				"secret",
				"set_owner",
			).Inc()
			return err
		}
		if err := r.Client.Create(ctx, mariadbSecret); err != nil {
			logger.Error(err, "Failed to create MariaDB secret")
			MariaDBReconcilerErrors.WithLabelValues(
				instance.Namespace,
				instance.Name,
				"secret",
				"create",
			).Inc()
			return err
		}
		logger.Info("MariaDB secret created successfully")
	} else if err != nil {
		// Other error fetching secret
		logger.Error(err, "Failed to get MariaDB secret")
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			"secret",
			"get",
		).Inc()
		return err
	} else {
		// Secret exists, ensure it has the right labels
		if mariadbSecret.Labels == nil {
			mariadbSecret.Labels = make(map[string]string)
		}
		changed := false
		for k, v := range r.generateLabelsForComponent(instance, "mariadb") {
			if mariadbSecret.Labels[k] != v {
				mariadbSecret.Labels[k] = v
				changed = true
			}
		}
		if changed {
			if err := r.Client.Update(ctx, mariadbSecret); err != nil {
				logger.Error(err, "Failed to update MariaDB secret labels")
				MariaDBReconcilerErrors.WithLabelValues(
					instance.Namespace,
					instance.Name,
					"secret",
					"update",
				).Inc()
				return err
			}
		}
	}

	return nil
}

// reconcileMariaDBServiceAccount creates or updates the MariaDB service account
func (r *GrafanaReconciler) reconcileMariaDBServiceAccount(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	mariadbServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "mariadb"),
		},
	}

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, mariadbServiceAccount, func() error {
		return ctrl.SetControllerReference(instance, mariadbServiceAccount, r.Scheme)
	})

	if err != nil {
		logger.Error(err, "Failed to reconcile MariaDB service account")
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			"serviceaccount",
			string(op),
		).Inc()
		return err
	}

	if op != ctrlutil.OperationResultNone {
		logger.Info("MariaDB service account reconciled", "operation", op)
	}

	return nil
}

// reconcileMariaDBPVC creates or updates the MariaDB PVC
func (r *GrafanaReconciler) reconcileMariaDBPVC(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	// Parse storage size from spec
	storageSize, err := resource.ParseQuantity(instance.Spec.MariaDB.StorageSize)
	if err != nil {
		logger.Error(err, "Failed to parse MariaDB storage size", "size", instance.Spec.MariaDB.StorageSize)
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			"pvc",
			"parse_storage",
		).Inc()
		return fmt.Errorf("invalid storage size %s: %w", instance.Spec.MariaDB.StorageSize, err)
	}

	mariadbPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
		},
	}

	pvcSpec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
		},
	}

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, mariadbPVC, func() error {
		// We don't update PVC spec after creation as it's immutable
		if mariadbPVC.CreationTimestamp.IsZero() {
			mariadbPVC.Spec = pvcSpec
		}
		mariadbPVC.Labels = r.generateLabelsForComponent(instance, "mariadb")
		return ctrl.SetControllerReference(instance, mariadbPVC, r.Scheme)
	})

	if err != nil {
		logger.Error(err, "Failed to reconcile MariaDB PVC")
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			"pvc",
			string(op),
		).Inc()
		return err
	}

	if op != ctrlutil.OperationResultNone {
		logger.Info("MariaDB PVC reconciled", "operation", op)
	}

	return nil
}

// reconcileMariaDBService creates or updates the MariaDB service
func (r *GrafanaReconciler) reconcileMariaDBService(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	mariadbService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
		},
	}

	serviceSpec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name:       "mysql",
				Port:       3306,
				TargetPort: intstr.FromInt(3306),
			},
		},
		Selector: r.generateLabelsForComponent(instance, "mariadb"),
	}

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, mariadbService, func() error {
		mariadbService.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "mariadb")
		mariadbService.Spec = serviceSpec
		return ctrl.SetControllerReference(instance, mariadbService, r.Scheme)
	})

	if err != nil {
		logger.Error(err, "Failed to reconcile MariaDB service")
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			"service",
			string(op),
		).Inc()
		return err
	}

	if op != ctrlutil.OperationResultNone {
		logger.Info("MariaDB service reconciled", "operation", op)
	}

	return nil
}

// reconcileMariaDBDeployment creates or updates the MariaDB deployment
func (r *GrafanaReconciler) reconcileMariaDBDeployment(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	secretName := r.generateNameForComponent(instance, "mariadb")
	pvcName := r.generateNameForComponent(instance, "mariadb")
	saName := r.generateNameForComponent(instance, "mariadb")

	mariaDBDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
		},
	}

	mariaDBDeploymentSpec := r.buildMariaDBDeploymentSpec(instance, secretName, pvcName, saName)

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, mariaDBDeployment, func() error {
		mariaDBDeployment.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "mariadb")
		mariaDBDeployment.Spec = mariaDBDeploymentSpec
		return ctrl.SetControllerReference(instance, mariaDBDeployment, r.Scheme)
	})

	if err != nil {
		logger.Error(err, "Failed to reconcile MariaDB deployment")
		MariaDBReconcilerErrors.WithLabelValues(
			instance.Namespace,
			instance.Name,
			"deployment",
			string(op),
		).Inc()
		return err
	}

	if op != ctrlutil.OperationResultNone {
		logger.Info("MariaDB deployment reconciled", "operation", op)
	}

	return nil
}

// buildMariaDBDeploymentSpec creates the deployment spec for MariaDB
func (r *GrafanaReconciler) buildMariaDBDeploymentSpec(instance *grafoov1alpha1.Grafana, secretName, pvcName, saName string) appsv1.DeploymentSpec {
	labels := r.generateLabelsForComponent(instance, "mariadb")

	return appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "mariadb",
						Image: instance.Spec.MariaDB.Image,
						Env: []corev1.EnvVar{
							{
								Name: "MYSQL_USER",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										Key: "database-user",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
							{
								Name: "MYSQL_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										Key: "database-password",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
							{
								Name: "MYSQL_ROOT_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										Key: "database-root-password",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
							{
								Name: "MYSQL_DATABASE",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										Key: "database-name",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
						},
						ImagePullPolicy: corev1.PullIfNotPresent,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{
										"/bin/sh",
										"-i",
										"-c",
										"MYSQL_PWD=\"$MYSQL_PASSWORD\" mysqladmin -u $MYSQL_USER ping",
									},
								},
							},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{
										"/bin/sh",
										"-i",
										"-c",
										"MYSQL_PWD=\"$MYSQL_PASSWORD\" mysqladmin -u $MYSQL_USER ping",
									},
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 3306,
								Protocol:      corev1.ProtocolTCP,
								Name:          "mysql",
							},
						},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: boolPtr(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							RunAsNonRoot: boolPtr(true),
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "mariadb-data",
								MountPath: "/var/lib/mysql/data",
							},
							{
								Name:      "kube-api-access",
								MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
								ReadOnly:  true,
							},
						},
					},
				},
				ServiceAccountName: saName,
				Volumes: []corev1.Volume{
					{
						Name: "mariadb-data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvcName,
							},
						},
					},
					{
						Name: "kube-api-access",
						VolumeSource: corev1.VolumeSource{
							Projected: &corev1.ProjectedVolumeSource{
								DefaultMode: int32Ptr(420),
								Sources: []corev1.VolumeProjection{
									{
										ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
											ExpirationSeconds: int64Ptr(3607),
											Path:              "token",
										},
									},
									{
										ConfigMap: &corev1.ConfigMapProjection{
											Items: []corev1.KeyToPath{
												{
													Key:  "ca.crt",
													Path: "ca.crt",
												},
											},
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "kube-root-ca.crt",
											},
										},
									},
									{
										DownwardAPI: &corev1.DownwardAPIProjection{
											Items: []corev1.DownwardAPIVolumeFile{
												{
													FieldRef: &corev1.ObjectFieldSelector{
														APIVersion: "v1",
														FieldPath:  "metadata.namespace",
													},
													Path: "namespace",
												},
											},
										},
									},
									{
										ConfigMap: &corev1.ConfigMapProjection{
											Items: []corev1.KeyToPath{
												{
													Key:  "service-ca.crt",
													Path: "service-ca.crt",
												},
											},
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "openshift-service-ca.crt",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
