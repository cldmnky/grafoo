package controller

import (
	"context"

	"github.com/google/uuid"
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

func (r *GrafanaReconciler) ReconcileMariaDB(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling MariaDB")
	// Create a secret for MariaDB once
	mariadbSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: r.generateNameForComponent(instance, "mariadb"), Namespace: instance.Namespace}, mariadbSecret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Creating MariaDB secret")
			mariadbSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      r.generateNameForComponent(instance, "mariadb"),
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
			if err := r.Client.Create(ctx, mariadbSecret); err != nil {
				return err
			}
			// set the owner reference
			if err := ctrl.SetControllerReference(instance, mariadbSecret, r.Scheme); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// service account for MariaDB
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
		return err
	}
	if op != ctrlutil.OperationResultCreated && op != ctrlutil.OperationResultUpdated {
		logger.Info("MariaDB service account", "operation", op)
	}

	storageSize, err := resource.ParseQuantity(instance.Spec.MariaDB.StorageSize)
	if err != nil {
		return err
	}
	mariadbPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
		},
	}

	mariadbPVCSpec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
		},
	}
	op, err = CreateOrUpdateWithRetries(ctx, r.Client, mariadbPVC, func() error {
		mariadbPVC.Labels = r.generateLabelsForComponent(instance, "mariadb")
		mariadbPVC.Spec = mariadbPVCSpec
		return ctrl.SetControllerReference(instance, mariadbPVC, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op != ctrlutil.OperationResultCreated && op != ctrlutil.OperationResultUpdated {
		logger.Info("MariaDB PVC", "operation", op)
	}

	// Create a MariaDB service
	mariadbService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
		},
	}
	mariadbServiceSpec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name:       "mysql",
				Port:       3306,
				TargetPort: intstr.FromInt(3306),
			},
		},
		Selector: r.generateLabelsForComponent(instance, "mariadb"),
	}
	op, err = CreateOrUpdateWithRetries(ctx, r.Client, mariadbService, func() error {
		mariadbService.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "mariadb")
		mariadbService.Spec = mariadbServiceSpec
		return ctrl.SetControllerReference(instance, mariadbService, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op != ctrlutil.OperationResultCreated && op != ctrlutil.OperationResultUpdated {
		logger.Info("MariaDB service", "operation", op)
	}

	mariaDBDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "mariadb"),
			Namespace: instance.Namespace,
		},
	}

	mariaDBDeploymentSpec := appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: r.generateLabelsForComponent(instance, "mariadb"),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: r.generateLabelsForComponent(instance, "mariadb"),
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
											Name: mariadbSecret.Name,
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
											Name: mariadbSecret.Name,
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
											Name: mariadbSecret.Name,
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
											Name: mariadbSecret.Name,
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
				ServiceAccountName: mariadbServiceAccount.Name,
				Volumes: []corev1.Volume{
					{
						Name: "mariadb-data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: mariadbPVC.Name,
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

	op, err = CreateOrUpdateWithRetries(ctx, r.Client, mariaDBDeployment, func() error {
		mariaDBDeployment.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "mariadb")
		mariaDBDeployment.Spec = mariaDBDeploymentSpec
		return ctrl.SetControllerReference(instance, mariaDBDeployment, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op != ctrlutil.OperationResultCreated && op != ctrlutil.OperationResultUpdated {
		logger.Info("MariaDB deployment", "operation", op)
	}

	return nil
}
