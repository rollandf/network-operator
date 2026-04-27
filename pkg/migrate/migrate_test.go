/*
2023 NVIDIA CORPORATION & AFFILIATES

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

package migrate

import (
	goctx "context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mellanox/network-operator/pkg/consts"

	mellanoxv1alpha1 "github.com/Mellanox/network-operator/api/v1alpha1"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
)

//nolint:dupl
var _ = Describe("Migrate", func() {
	AfterEach(func() {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespaceName, Name: nvIPAMcmName}}
		_ = k8sClient.Delete(goctx.Background(), cm)
		_ = k8sClient.DeleteAllOf(goctx.Background(), &corev1.Node{})
		_ = k8sClient.DeleteAllOf(goctx.Background(), &corev1.Pod{})
		_ = k8sClient.Delete(goctx.Background(), &mellanoxv1alpha1.NicClusterPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: consts.NicClusterPolicyResourceName},
		})
	})
	It("should delete MOFED DS", func() {
		upgrade.SetDriverName("ofed")
		createNCP()
		createMofedDS()
		createNodes()
		createPods()
		By("Verify Single DS is deleted")
		err := Migrate(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			ds := &appsv1.DaemonSet{}
			err = k8sClient.Get(goctx.TODO(), types.NamespacedName{Namespace: namespaceName, Name: "test-ds"}, ds)
			return errors.IsNotFound(err)
		})
		By("Verify Nodes have upgrade-requested annotation")
		Eventually(func() bool {
			node1 := &corev1.Node{}
			err = k8sClient.Get(goctx.TODO(), types.NamespacedName{Namespace: namespaceName, Name: "test-node1"}, node1)
			Expect(err).NotTo(HaveOccurred())
			node2 := &corev1.Node{}
			err = k8sClient.Get(goctx.TODO(), types.NamespacedName{Namespace: namespaceName, Name: "test-node2"}, node2)
			Expect(err).NotTo(HaveOccurred())
			return node1.Annotations[upgrade.GetUpgradeRequestedAnnotationKey()] == "true" &&
				node2.Annotations[upgrade.GetUpgradeRequestedAnnotationKey()] == "true"
		})
	})
})

func createMofedDS() {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName,
			Name:      "test-ds",
			Labels:    map[string]string{"nvidia.com/ofed-driver": ""},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "mofed-ubuntu22.04"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "mofed-ubuntu22.04"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "mofed-container",
							Image: "github/mofed",
						},
					},
				},
			},
		},
	}
	err := k8sClient.Create(goctx.Background(), ds)
	Expect(err).NotTo(HaveOccurred())
}

func createNodes() {
	By("Create Nodes")
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node1",
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
	}
	err := k8sClient.Create(goctx.TODO(), node)
	Expect(err).NotTo(HaveOccurred())
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node2",
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
	}
	err = k8sClient.Create(goctx.TODO(), node2)
	Expect(err).NotTo(HaveOccurred())
}

func createPods() {
	By("Create Pods")
	gracePeriodSeconds := int64(0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod1",
			Namespace: namespaceName,
			Labels:    map[string]string{"app": "mofed-ubuntu22.04"},
		},
		Spec: corev1.PodSpec{
			NodeName:                      "test-node1",
			TerminationGracePeriodSeconds: &gracePeriodSeconds,
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}
	err := k8sClient.Create(goctx.TODO(), pod)
	Expect(err).NotTo(HaveOccurred())
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod2",
			Namespace: namespaceName,
			Labels:    map[string]string{"app": "mofed-ubuntu22.04"},
		},
		Spec: corev1.PodSpec{
			NodeName:                      "test-node2",
			TerminationGracePeriodSeconds: &gracePeriodSeconds,
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}
	err = k8sClient.Create(goctx.TODO(), pod2)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("handleMissingDSOwnerLabelOnPods", func() {
	var ncp *mellanoxv1alpha1.NicClusterPolicy

	BeforeEach(func() {
		ncp = &mellanoxv1alpha1.NicClusterPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: consts.NicClusterPolicyResourceName},
			Spec: mellanoxv1alpha1.NicClusterPolicySpec{
				OFEDDriver: &mellanoxv1alpha1.OFEDDriverSpec{
					ImageSpec: mellanoxv1alpha1.ImageSpec{
						Image:      "mofed",
						Repository: "nvcr.io/nvidia/mellanox",
						Version:    "5.9-0.5.6.0",
					},
				},
			},
		}
	})

	AfterEach(func() {
		_ = k8sClient.DeleteAllOf(goctx.Background(), &corev1.Pod{}, client.InNamespace(namespaceName))
		if ncp != nil {
			_ = k8sClient.Delete(goctx.Background(), ncp)
		}
	})

	It("should backfill ds-owner on old OFED pods that lack the label", func() {
		Expect(k8sClient.Create(goctx.Background(), ncp)).To(Succeed())

		pod := ofedPodWithoutDSOwner("old-pod-1")
		Expect(k8sClient.Create(goctx.Background(), pod)).To(Succeed())

		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "old-pod-1"}, updated)).To(Succeed())
		Expect(updated.Labels[consts.DSOwnerLabel]).To(Equal(mellanoxv1alpha1.NicClusterPolicyCRDName))
	})

	It("should be a no-op when pods already have the ds-owner label", func() {
		Expect(k8sClient.Create(goctx.Background(), ncp)).To(Succeed())

		pod := ofedPodWithoutDSOwner("new-pod-1")
		pod.Labels[consts.DSOwnerLabel] = mellanoxv1alpha1.NicClusterPolicyCRDName
		Expect(k8sClient.Create(goctx.Background(), pod)).To(Succeed())

		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "new-pod-1"}, updated)).To(Succeed())
		Expect(updated.Labels[consts.DSOwnerLabel]).To(Equal(mellanoxv1alpha1.NicClusterPolicyCRDName))
	})

	It("should not touch NNP pods that already have a different ds-owner value", func() {
		Expect(k8sClient.Create(goctx.Background(), ncp)).To(Succeed())

		pod := ofedPodWithoutDSOwner("nnp-pod-1")
		pod.Labels[consts.DSOwnerLabel] = "nnp-my-policy"
		Expect(k8sClient.Create(goctx.Background(), pod)).To(Succeed())

		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "nnp-pod-1"}, updated)).To(Succeed())
		Expect(updated.Labels[consts.DSOwnerLabel]).To(Equal("nnp-my-policy"))
	})

	It("should be a no-op when NicClusterPolicy does not exist", func() {
		ncp = nil
		pod := ofedPodWithoutDSOwner("orphan-pod-1")
		Expect(k8sClient.Create(goctx.Background(), pod)).To(Succeed())

		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "orphan-pod-1"}, updated)).To(Succeed())
		_, hasLabel := updated.Labels[consts.DSOwnerLabel]
		Expect(hasLabel).To(BeFalse())
	})

	It("should only patch pods missing ds-owner when old and new pods coexist", func() {
		Expect(k8sClient.Create(goctx.Background(), ncp)).To(Succeed())

		// Simulates the real upgrade scenario: one old pod (no ds-owner) and one already-labeled pod.
		oldPod := ofedPodWithoutDSOwner("mixed-old-pod")
		Expect(k8sClient.Create(goctx.Background(), oldPod)).To(Succeed())

		newPod := ofedPodWithoutDSOwner("mixed-new-pod")
		newPod.Labels[consts.DSOwnerLabel] = mellanoxv1alpha1.NicClusterPolicyCRDName
		Expect(k8sClient.Create(goctx.Background(), newPod)).To(Succeed())

		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		// Old pod must now have the label.
		updatedOld := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "mixed-old-pod"}, updatedOld)).To(Succeed())
		Expect(updatedOld.Labels[consts.DSOwnerLabel]).To(Equal(mellanoxv1alpha1.NicClusterPolicyCRDName))

		// Already-labeled pod must be unchanged (same value, not overwritten).
		updatedNew := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "mixed-new-pod"}, updatedNew)).To(Succeed())
		Expect(updatedNew.Labels[consts.DSOwnerLabel]).To(Equal(mellanoxv1alpha1.NicClusterPolicyCRDName))
		Expect(updatedNew.ResourceVersion).To(Equal(newPod.ResourceVersion),
			"already-labeled pod should not have been patched (ResourceVersion unchanged)")
	})

	It("should skip a pod that is deleted between List and Patch without failing", func() {
		Expect(k8sClient.Create(goctx.Background(), ncp)).To(Succeed())

		// Create one pod that will be deleted before the patch, and one that should survive.
		podToDelete := ofedPodWithoutDSOwner("deleted-pod-1")
		Expect(k8sClient.Create(goctx.Background(), podToDelete)).To(Succeed())
		podToKeep := ofedPodWithoutDSOwner("kept-pod-1")
		Expect(k8sClient.Create(goctx.Background(), podToKeep)).To(Succeed())

		// Delete the first pod to simulate it disappearing between List and Patch.
		Expect(k8sClient.Delete(goctx.Background(), podToDelete)).To(Succeed())

		// Migration must not fail and must still patch the surviving pod.
		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "kept-pod-1"}, updated)).To(Succeed())
		Expect(updated.Labels[consts.DSOwnerLabel]).To(Equal(mellanoxv1alpha1.NicClusterPolicyCRDName))
	})

	It("should be a no-op when NicClusterPolicy has no OFED driver configured", func() {
		ncp.Spec.OFEDDriver = nil
		Expect(k8sClient.Create(goctx.Background(), ncp)).To(Succeed())

		pod := ofedPodWithoutDSOwner("no-ofed-pod-1")
		Expect(k8sClient.Create(goctx.Background(), pod)).To(Succeed())

		err := handleMissingDSOwnerLabelOnPods(goctx.Background(), testLog, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.Pod{}
		Expect(k8sClient.Get(goctx.Background(),
			types.NamespacedName{Namespace: namespaceName, Name: "no-ofed-pod-1"}, updated)).To(Succeed())
		_, hasLabel := updated.Labels[consts.DSOwnerLabel]
		Expect(hasLabel).To(BeFalse())
	})
})

// ofedPodWithoutDSOwner creates a pod spec that mimics a v26.1 GA MOFED pod:
// it has the nvidia.com/ofed-driver label but no ds-owner label.
func ofedPodWithoutDSOwner(name string) *corev1.Pod {
	gracePeriod := int64(0)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespaceName,
			Labels: map[string]string{
				consts.OfedDriverLabel: "",
			},
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &gracePeriod,
			Containers: []corev1.Container{
				{Name: "mofed-container", Image: "nvcr.io/nvidia/mellanox/doca-driver:test"},
			},
		},
	}
}

func createNCP() {
	ncp := &mellanoxv1alpha1.NicClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: consts.NicClusterPolicyResourceName}}
	ncp.Spec.OFEDDriver = &mellanoxv1alpha1.OFEDDriverSpec{
		ImageSpec: mellanoxv1alpha1.ImageSpec{
			Image:            "mofed",
			Repository:       "nvcr.io/nvidia/mellanox",
			Version:          "5.9-0.5.6.0",
			ImagePullSecrets: []string{},
		},
	}
	err := k8sClient.Create(goctx.Background(), ncp)
	Expect(err).NotTo(HaveOccurred())
}
