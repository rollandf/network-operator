/*
2024 NVIDIA CORPORATION & AFFILIATES

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

package state_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/gomega"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"

	mellanoxv1alpha1 "github.com/Mellanox/network-operator/api/v1alpha1"
	clustertype_mocks "github.com/Mellanox/network-operator/pkg/clustertype/mocks"
	"github.com/Mellanox/network-operator/pkg/consts"
	"github.com/Mellanox/network-operator/pkg/state"
	"github.com/Mellanox/network-operator/pkg/staticconfig"
	staticconfig_mocks "github.com/Mellanox/network-operator/pkg/staticconfig/mocks"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	hostDeviceNetworkResourceNamePrefix = "nvidia.com/"
	defaultTestRepository               = "myRepo"
	defaultTestImage                    = "myImage"
	defaultTestVersion                  = "myVersion"
)

var testLogger = log.Log.WithName("testLog")

func getTestCatalog() state.InfoCatalog {
	return getTestCatalogForOpenshift(false)
}

func getOpenshiftTestCatalog() state.InfoCatalog {
	return getTestCatalogForOpenshift(true)
}

func getTestCatalogForOpenshift(isOpenshift bool) state.InfoCatalog {
	catalog := state.NewInfoCatalog()
	clusterTypeProvider := clustertype_mocks.Provider{}
	clusterTypeProvider.On("IsOpenshift").Return(isOpenshift)
	staticConfigProvider := staticconfig_mocks.Provider{}
	staticConfigProvider.On("GetStaticConfig").Return(staticconfig.StaticConfig{CniBinDirectory: ""})
	catalog.Add(state.InfoTypeStaticConfig, &staticConfigProvider)
	catalog.Add(state.InfoTypeClusterType, &clusterTypeProvider)
	return catalog
}

type nadConfigIPAM struct {
	Type    string   `json:"type"`
	Range   string   `json:"range"`
	Exclude []string `json:"exclude"`
}

type nadConfig struct {
	CNIVersion string        `json:"cniVersion"`
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Master     string        `json:"master"`
	Mode       string        `json:"mode"`
	MTU        int           `json:"mtu"`
	IPAM       nadConfigIPAM `json:"ipam"`
}

func defaultNADConfig(cfg *nadConfig) nadConfig {
	return nadConfig{
		CNIVersion: "0.3.1",
		Name:       cfg.Name,
		Type:       cfg.Type,
		Master:     cfg.Master,
		Mode:       cfg.Mode,
		IPAM:       cfg.IPAM,
		MTU:        cfg.MTU,
	}
}

func getNADConfig(jsonData string) nadConfig {
	config := &nadConfig{}
	err := json.Unmarshal([]byte(jsonData), &config)
	Expect(err).To(BeNil())
	return *config
}

func getNADConfigIPAMJSON(ipam nadConfigIPAM) string {
	ipamJSON, err := json.Marshal(ipam)
	Expect(err).To(BeNil())
	return string(ipamJSON)
}

func assertCommonPodTemplateFields(template *corev1.PodTemplateSpec, image *mellanoxv1alpha1.ImageSpec) {
	// Image name
	Expect(template.Spec.Containers[0].Image).To(Equal(
		fmt.Sprintf("%v/%v:%v", image.Repository, image.Image, image.Version)),
	)

	// ImagePullSecrets
	Expect(template.Spec.ImagePullSecrets).To(ConsistOf(
		corev1.LocalObjectReference{Name: "secret-one"},
		corev1.LocalObjectReference{Name: "secret-two"},
	))

	// Container Resources
	Expect(template.Spec.Containers[0].Resources.Limits).To(Equal(image.ContainerResources[0].Limits))
	Expect(template.Spec.Containers[0].Resources.Requests).To(Equal(image.ContainerResources[0].Requests))

	Expect(template.Spec.Tolerations).To(ContainElements(
		corev1.Toleration{
			Key:               "nvidia.com/gpu",
			Operator:          "Exists",
			Value:             "",
			Effect:            "NoSchedule",
			TolerationSeconds: nil,
		},
	))
}

func assertCommonDeploymentFieldsFromUnstructured(u *unstructured.Unstructured, image *mellanoxv1alpha1.ImageSpec) {
	d := &appsv1.Deployment{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), d)
	Expect(err).ToNot(HaveOccurred())
	assertCommonDeploymentFields(d, image)
}

func assertCommonDeploymentFields(d *appsv1.Deployment, image *mellanoxv1alpha1.ImageSpec) {
	assertCommonPodTemplateFields(&d.Spec.Template, image)
}

func assertCommonDaemonSetFieldsFromUnstructured(u *unstructured.Unstructured,
	image *mellanoxv1alpha1.ImageSpec, policy *mellanoxv1alpha1.NicClusterPolicy) {
	ds := &appsv1.DaemonSet{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), ds)
	Expect(err).ToNot(HaveOccurred())
	assertCommonDaemonSetFields(ds, image, policy)
}

func assertCommonDaemonSetFields(ds *appsv1.DaemonSet, image *mellanoxv1alpha1.ImageSpec,
	policy *mellanoxv1alpha1.NicClusterPolicy) {
	assertCommonPodTemplateFields(&ds.Spec.Template, image)

	Expect(ds.Spec.Template.Spec.Tolerations).To(ContainElements(
		corev1.Toleration{Key: "first-taint"},
	))
	Expect(ds.Spec.Template.Spec.Affinity.NodeAffinity).To(Equal(policy.Spec.NodeAffinity))
}

func getTestImageSpec() *mellanoxv1alpha1.ImageSpec {
	return &mellanoxv1alpha1.ImageSpec{
		Image:            defaultTestImage,
		Repository:       defaultTestRepository,
		Version:          defaultTestVersion,
		ImagePullSecrets: []string{"secret-one", "secret-two"},
	}
}

func addContainerResources(imageSpec *mellanoxv1alpha1.ImageSpec,
	containerName, requestValue, limitValue string) *mellanoxv1alpha1.ImageSpec {
	i := imageSpec.DeepCopy()
	i.ContainerResources = append(i.ContainerResources, []mellanoxv1alpha1.ResourceRequirements{
		{
			Name:     containerName,
			Limits:   map[corev1.ResourceName]resource.Quantity{"resource-one": resource.MustParse(limitValue)},
			Requests: map[corev1.ResourceName]resource.Quantity{"resource-one": resource.MustParse(requestValue)},
		},
	}...)
	return i
}

func isNamespaced(obj *unstructured.Unstructured) bool {
	return obj.GetKind() != "CustomResourceDefinition" &&
		obj.GetKind() != "ClusterRole" &&
		obj.GetKind() != "ClusterRoleBinding" &&
		obj.GetKind() != "ValidatingWebhookConfiguration"
}

func assertCNIBinDirForDSFromUnstructured(u *unstructured.Unstructured) {
	ds := &appsv1.DaemonSet{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), ds)
	Expect(err).ToNot(HaveOccurred())
	assertCNIBinDirForDS(ds)
}

func assertCNIBinDirForDS(ds *appsv1.DaemonSet) {
	for i := range ds.Spec.Template.Spec.Volumes {
		vol := ds.Spec.Template.Spec.Volumes[i]
		if vol.Name == "cnibin" {
			Expect(vol.HostPath).NotTo(BeNil())
			Expect(vol.HostPath.Path).To(Equal("custom-cni-bin-directory"))
		}
	}
}

func assertNetworkAttachmentDefinition(c client.Client, expectedNadConfig *nadConfig,
	name, namespace, resourceName string) {
	nad := &netattdefv1.NetworkAttachmentDefinition{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, nad)
	Expect(err).NotTo(HaveOccurred())

	convertedNadConfig := getNADConfig(nad.Spec.Config)
	Expect(convertedNadConfig).To(BeEquivalentTo(*expectedNadConfig))

	Expect(nad.Name).To(Equal(name))
	Expect(nad.Namespace).To(Equal(namespace))

	if resourceName != "" {
		resourceNameAnnotation, ok := nad.Annotations["k8s.v1.cni.cncf.io/resourceName"]
		Expect(ok).To(BeTrue())
		Expect(resourceNameAnnotation).To(Equal(hostDeviceNetworkResourceNamePrefix + resourceName))
	}
}

func GetManifestObjectsTest(ctx context.Context, cr *mellanoxv1alpha1.NicClusterPolicy, catalog state.InfoCatalog,
	imageSpec *mellanoxv1alpha1.ImageSpec, renderer state.ManifestRenderer) {
	got, err := renderer.GetManifestObjects(ctx, cr, catalog, log.FromContext(ctx))
	Expect(err).ToNot(HaveOccurred())
	for i := range got {
		if isNamespaced(got[i]) {
			Expect(got[i].GetNamespace()).To(Equal("nvidia-network-operator"))
		}
		switch got[i].GetKind() {
		case "DaemonSet":
			assertCommonDaemonSetFieldsFromUnstructured(got[i], imageSpec, cr)
			assertCNIBinDirForDSFromUnstructured(got[i])
		case "Deployment":
			assertCommonDeploymentFieldsFromUnstructured(got[i], imageSpec)
		}
	}
}

func getTestClusterPolicyWithBaseFields() *mellanoxv1alpha1.NicClusterPolicy {
	return &mellanoxv1alpha1.NicClusterPolicy{
		Spec: mellanoxv1alpha1.NicClusterPolicySpec{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{{
								Key:      "node-label",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"labels"},
							},
							},
						},
					},
				},
				PreferredDuringSchedulingIgnoredDuringExecution: nil,
			},
			Tolerations: []corev1.Toleration{{Key: "first-taint"}},
		},
	}
}

func getKindState(ctx context.Context, c client.Client, objs []*unstructured.Unstructured,
	targetKind string) (state.SyncState, error) {
	reqLogger := log.FromContext(ctx)
	reqLogger.V(consts.LogLevelInfo).Info("Checking related object states")
	for _, obj := range objs {
		if obj.GetKind() != targetKind {
			continue
		}
		found := obj.DeepCopy()
		err := c.Get(
			ctx, types.NamespacedName{Name: found.GetName(), Namespace: found.GetNamespace()}, found)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return state.SyncStateNotReady, nil
			}
			return state.SyncStateNotReady, fmt.Errorf("failed to get object: %w", err)
		}

		buf, err := found.MarshalJSON()
		if err != nil {
			return state.SyncStateNotReady, fmt.Errorf("failed to marshall unstructured daemonset object: %w", err)
		}

		switch obj.GetKind() {
		case "DaemonSet":
			ds := &appsv1.DaemonSet{}
			if err = json.Unmarshal(buf, ds); err != nil {
				return state.SyncStateNotReady, fmt.Errorf("failed to unmarshall to daemonset object: %w", err)
			}
			if ds.Status.DesiredNumberScheduled != 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberAvailable &&
				ds.Status.UpdatedNumberScheduled == ds.Status.NumberAvailable {
				return state.SyncStateReady, nil
			}
			return state.SyncStateNotReady, nil
		case "Deployment":
			d := &appsv1.Deployment{}
			if err = json.Unmarshal(buf, d); err != nil {
				return state.SyncStateNotReady, fmt.Errorf("failed to unmarshall to deployment object: %w", err)
			}

			if d.Status.ObservedGeneration > 0 && d.Status.Replicas == d.Status.ReadyReplicas &&
				d.Status.UpdatedReplicas == d.Status.AvailableReplicas {
				return state.SyncStateReady, nil
			}
			return state.SyncStateNotReady, nil
		default:
			return state.SyncStateNotReady, fmt.Errorf("unsupported target kind")
		}
	}
	return state.SyncStateNotReady, fmt.Errorf("objects list does not contain the specified target kind")
}
