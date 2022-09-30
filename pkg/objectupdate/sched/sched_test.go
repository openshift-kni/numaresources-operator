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

package sched

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
)

const (
	schedConfig = `apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
  - schedulerName: test-topo-aware-sched
    plugins:
      filter:
        enabled:
          - name: NodeResourceTopologyMatch
      reserve:
        enabled:
          - name: NodeResourceTopologyMatch
      score:
        enabled:
          - name: NodeResourceTopologyMatch
    # optional plugin configs
    pluginConfig:
    - name: NodeResourceTopologyMatch
      args:
        scoringStrategy:
          type: LeastAllocated`

	schedConfigWithParams = `apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
  - schedulerName: test-topo-aware-sched
    plugins:
      filter:
        enabled:
          - name: NodeResourceTopologyMatch
      reserve:
        enabled:
          - name: NodeResourceTopologyMatch
      score:
        enabled:
          - name: NodeResourceTopologyMatch
    # optional plugin configs
    pluginConfig:
    - name: NodeResourceTopologyMatch
      args:
        apiVersion: kubescheduler.config.k8s.io/v1beta2
        cacheResyncPeriodSeconds: 3
        kind: NodeResourceTopologyMatchArgs
        scoringStrategy:
          resources:
          - name: cpu
            weight: 1
          - name: memory
            weight: 1
          type: LeastAllocated`

	schedConfigWithPeriod = `apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
  - schedulerName: test-topo-aware-sched
    plugins:
      filter:
        enabled:
          - name: NodeResourceTopologyMatch
      reserve:
        enabled:
          - name: NodeResourceTopologyMatch
      score:
        enabled:
          - name: NodeResourceTopologyMatch
    # optional plugin configs
    pluginConfig:
    - name: NodeResourceTopologyMatch
      args:
        cacheResyncPeriodSeconds: 10
        scoringStrategy:
          type: LeastAllocated`
)

var dp = &appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-deployent",
		Namespace: "test-namespace",
	},
	Spec: appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "quay.io/bar/image:v1",
					},
				},
				Volumes: []corev1.Volume{
					schedstate.NewSchedConfigVolume("foo", "bar"),
				},
			},
		},
	},
}

func TestUpdateDeploymentImageSettings(t *testing.T) {
	type testCase struct {
		imageSpec string
	}

	testCases := []testCase{
		{
			imageSpec: "quay.io/bar/image:v2",
		},
		{
			imageSpec: "quay.io/bar/image:v3",
		},
		{
			imageSpec: "quay.io/foo/image:v2",
		},
	}

	podSpec := &dp.Spec.Template.Spec
	for _, tc := range testCases {
		DeploymentImageSettings(dp, tc.imageSpec)
		if podSpec.Containers[0].Image != tc.imageSpec {
			t.Errorf("failed to update deployemt image, expected: %q actual: %q", tc.imageSpec, podSpec.Containers[0].Image)
		}
	}
}

func TestUpdateDeploymentConfigMapSettings(t *testing.T) {
	type testCase struct {
		cmName string
		cmHash string
	}

	testCases := []testCase{
		{
			cmName: "cm1",
			cmHash: "SHA256:c73d08de890479518ed60cf670d17faa26a4a71f995c1dcc978165399401a6c4",
		},
		{
			cmName: "cm5",
			cmHash: "SHA256:eb368a2dfd38b405f014118c7d9747fcc97f4f0ee75c05963cd9da6ee65ef498",
		},
		{
			cmName: "cm3",
			cmHash: "SHA256:a4bd99e1e0aba51814e81388badb23ecc560312c4324b2018ea76393ea1caca9",
		},
	}

	podSpec := &dp.Spec.Template.Spec
	for _, tc := range testCases {
		DeploymentConfigMapSettings(dp, tc.cmName, tc.cmHash)
		if podSpec.Volumes[0].Name != schedstate.SchedulerConfigMapVolumeName {
			t.Errorf("failed to update deployment volume name, expected: %q actual: %q", schedstate.SchedulerConfigMapVolumeName, podSpec.Volumes[0].Name)
		}
		if podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name != tc.cmName {
			t.Errorf("failed to update deployment volume configmap name, expected: %q actual: %q", tc.cmName, podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name)
		}
		val, ok := dp.Spec.Template.Annotations[hash.ConfigMapAnnotation]
		if !ok {
			t.Errorf("failed to update deployment: %q with annotation key: %q", fmt.Sprintf("%s/%s", dp.Namespace, dp.Name), hash.ConfigMapAnnotation)
		}
		if val != tc.cmHash {
			t.Errorf("failed to update deployment: %q with correct value in annotation %q, expected: %q, actual: %q", fmt.Sprintf("%s/%s", dp.Namespace, dp.Name), hash.ConfigMapAnnotation, tc.cmHash, val)
		}
	}
}

func TestUpdateSchedulerName(t *testing.T) {
	type testCase struct {
		name          string
		schedulerName string
		expectedName  string
		isErrExpected bool
		configMap     corev1.ConfigMap
	}

	testCases := []testCase{
		{
			name:          "with-empty-name",
			schedulerName: "",
			expectedName:  "test-topo-aware-sched",
			isErrExpected: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
		},
		{
			name:          "with-name",
			schedulerName: "foo",
			expectedName:  "foo",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SchedulerConfig(&tc.configMap, tc.schedulerName, 0); err != nil {
				if !tc.isErrExpected {
					t.Errorf("test %q: failed with error: %v", tc.name, err)
				}
			}
			gotName, found := schedstate.SchedulerNameFromObject(&tc.configMap)
			if !found {
				t.Errorf("test %q: did not find data", tc.name)
			}
			if gotName != tc.expectedName {
				t.Errorf("test %q: find name: expected=%q got=%q", tc.name, tc.expectedName, gotName)
			}
		})
	}
}

func TestUpdateSchedulerConfig(t *testing.T) {
	type testCase struct {
		name              string
		cacheResyncPeriod time.Duration
		isErrExpected     bool
		configMap         corev1.ConfigMap
		expectedYAML      string
	}

	testCases := []testCase{
		{
			name:              "enable-reserve",
			cacheResyncPeriod: 3 * time.Second,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
			expectedYAML: expectedYAMLWithReconcilePeriod,
		},
		{
			name:              "tune-reserve",
			cacheResyncPeriod: 3 * time.Second,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigWithPeriod,
				},
			},
			expectedYAML: expectedYAMLWithReconcilePeriod,
		},
		{
			name:              "keep-reserve-enabled",
			cacheResyncPeriod: 3 * time.Second,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigWithParams,
				},
			},
			expectedYAML: expectedYAMLWithReconcilePeriod,
		},
		{
			name: "keep-reserve-disabled",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
			expectedYAML: expectedYAMLWithoutReconcile,
		},
		{
			name: "disable-reconcile",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigWithParams,
				},
			},
			expectedYAML: expectedYAMLWithoutReconcile,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SchedulerConfig(&tc.configMap, "", tc.cacheResyncPeriod); err != nil {
				if !tc.isErrExpected {
					t.Errorf("test %q: failed with error: %v", tc.name, err)
				}
			}

			gotYAML, ok := tc.configMap.Data[schedstate.SchedulerConfigFileName]
			if !ok {
				t.Fatalf("test %q failed: malformed config map", tc.name)
			}

			yamlCompare(t, tc.name, gotYAML, tc.expectedYAML)
		})
	}
}

func yamlCompare(t *testing.T, testName, got, expected string) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(got, expected, true)
	diffCount := 0
	for idx, hunk := range diffs {
		if hunk.Type == diffmatchpatch.DiffEqual {
			continue
		}
		t.Errorf("test %q diff %d: op=%s text=%q\n", testName, idx, hunk.Type.String(), hunk.Text)
	}
	if diffCount > 0 {
		var err error
		err = os.WriteFile("got.yaml", []byte(got), 0644)
		if err != nil {
			t.Fatalf("cannot write got.yaml")
		}
		err = os.WriteFile("exp.yaml", []byte(expected), 0644)
		if err != nil {
			t.Fatalf("cannot write exp.yaml")
		}
	}
}

const (
	expectedYAMLWithReconcilePeriod = `apiVersion: kubescheduler.config.k8s.io/v1beta2
clientConnection:
  acceptContentTypes: ""
  burst: 100
  contentType: application/vnd.kubernetes.protobuf
  kubeconfig: ""
  qps: 50
enableContentionProfiling: true
enableProfiling: true
healthzBindAddress: ""
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
  leaseDuration: 15s
  renewDeadline: 10s
  resourceLock: leases
  resourceName: kube-scheduler
  resourceNamespace: kube-system
  retryPeriod: 2s
metricsBindAddress: ""
parallelism: 16
percentageOfNodesToScore: 0
podInitialBackoffSeconds: 1
podMaxBackoffSeconds: 10
profiles:
- pluginConfig:
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      cacheResyncPeriodSeconds: 3
      kind: NodeResourceTopologyMatchArgs
      scoringStrategy:
        resources:
        - name: cpu
          weight: 1
        - name: memory
          weight: 1
        type: LeastAllocated
    name: NodeResourceTopologyMatch
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: DefaultPreemptionArgs
      minCandidateNodesAbsolute: 100
      minCandidateNodesPercentage: 10
    name: DefaultPreemption
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      hardPodAffinityWeight: 1
      kind: InterPodAffinityArgs
    name: InterPodAffinity
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: NodeAffinityArgs
    name: NodeAffinity
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: NodeResourcesBalancedAllocationArgs
      resources:
      - name: cpu
        weight: 1
      - name: memory
        weight: 1
    name: NodeResourcesBalancedAllocation
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: NodeResourcesFitArgs
      scoringStrategy:
        resources:
        - name: cpu
          weight: 1
        - name: memory
          weight: 1
        type: LeastAllocated
    name: NodeResourcesFit
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      defaultingType: System
      kind: PodTopologySpreadArgs
    name: PodTopologySpread
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      bindTimeoutSeconds: 600
      kind: VolumeBindingArgs
    name: VolumeBinding
  plugins:
    bind:
      enabled:
      - name: DefaultBinder
        weight: 0
    filter:
      enabled:
      - name: NodeUnschedulable
        weight: 0
      - name: NodeName
        weight: 0
      - name: TaintToleration
        weight: 0
      - name: NodeAffinity
        weight: 0
      - name: NodePorts
        weight: 0
      - name: NodeResourcesFit
        weight: 0
      - name: VolumeRestrictions
        weight: 0
      - name: EBSLimits
        weight: 0
      - name: GCEPDLimits
        weight: 0
      - name: NodeVolumeLimits
        weight: 0
      - name: AzureDiskLimits
        weight: 0
      - name: VolumeBinding
        weight: 0
      - name: VolumeZone
        weight: 0
      - name: PodTopologySpread
        weight: 0
      - name: InterPodAffinity
        weight: 0
      - name: NodeResourceTopologyMatch
        weight: 0
    multiPoint: {}
    permit: {}
    postBind: {}
    postFilter:
      enabled:
      - name: DefaultPreemption
        weight: 0
    preBind:
      enabled:
      - name: VolumeBinding
        weight: 0
    preFilter:
      enabled:
      - name: NodeResourcesFit
        weight: 0
      - name: NodePorts
        weight: 0
      - name: VolumeRestrictions
        weight: 0
      - name: PodTopologySpread
        weight: 0
      - name: InterPodAffinity
        weight: 0
      - name: VolumeBinding
        weight: 0
      - name: NodeAffinity
        weight: 0
    preScore:
      enabled:
      - name: InterPodAffinity
        weight: 0
      - name: PodTopologySpread
        weight: 0
      - name: TaintToleration
        weight: 0
      - name: NodeAffinity
        weight: 0
    queueSort:
      enabled:
      - name: PrioritySort
        weight: 0
    reserve:
      enabled:
      - name: VolumeBinding
        weight: 0
      - name: NodeResourceTopologyMatch
        weight: 0
    score:
      enabled:
      - name: NodeResourcesBalancedAllocation
        weight: 1
      - name: ImageLocality
        weight: 1
      - name: InterPodAffinity
        weight: 1
      - name: NodeResourcesFit
        weight: 1
      - name: NodeAffinity
        weight: 1
      - name: PodTopologySpread
        weight: 2
      - name: TaintToleration
        weight: 1
      - name: NodeResourceTopologyMatch
        weight: 0
  schedulerName: test-topo-aware-sched
`
	expectedYAMLWithoutReconcile = `apiVersion: kubescheduler.config.k8s.io/v1beta2
clientConnection:
  acceptContentTypes: ""
  burst: 100
  contentType: application/vnd.kubernetes.protobuf
  kubeconfig: ""
  qps: 50
enableContentionProfiling: true
enableProfiling: true
healthzBindAddress: ""
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
  leaseDuration: 15s
  renewDeadline: 10s
  resourceLock: leases
  resourceName: kube-scheduler
  resourceNamespace: kube-system
  retryPeriod: 2s
metricsBindAddress: ""
parallelism: 16
percentageOfNodesToScore: 0
podInitialBackoffSeconds: 1
podMaxBackoffSeconds: 10
profiles:
- pluginConfig:
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      cacheResyncPeriodSeconds: 0
      kind: NodeResourceTopologyMatchArgs
      scoringStrategy:
        resources:
        - name: cpu
          weight: 1
        - name: memory
          weight: 1
        type: LeastAllocated
    name: NodeResourceTopologyMatch
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: DefaultPreemptionArgs
      minCandidateNodesAbsolute: 100
      minCandidateNodesPercentage: 10
    name: DefaultPreemption
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      hardPodAffinityWeight: 1
      kind: InterPodAffinityArgs
    name: InterPodAffinity
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: NodeAffinityArgs
    name: NodeAffinity
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: NodeResourcesBalancedAllocationArgs
      resources:
      - name: cpu
        weight: 1
      - name: memory
        weight: 1
    name: NodeResourcesBalancedAllocation
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      kind: NodeResourcesFitArgs
      scoringStrategy:
        resources:
        - name: cpu
          weight: 1
        - name: memory
          weight: 1
        type: LeastAllocated
    name: NodeResourcesFit
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      defaultingType: System
      kind: PodTopologySpreadArgs
    name: PodTopologySpread
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta2
      bindTimeoutSeconds: 600
      kind: VolumeBindingArgs
    name: VolumeBinding
  plugins:
    bind:
      enabled:
      - name: DefaultBinder
        weight: 0
    filter:
      enabled:
      - name: NodeUnschedulable
        weight: 0
      - name: NodeName
        weight: 0
      - name: TaintToleration
        weight: 0
      - name: NodeAffinity
        weight: 0
      - name: NodePorts
        weight: 0
      - name: NodeResourcesFit
        weight: 0
      - name: VolumeRestrictions
        weight: 0
      - name: EBSLimits
        weight: 0
      - name: GCEPDLimits
        weight: 0
      - name: NodeVolumeLimits
        weight: 0
      - name: AzureDiskLimits
        weight: 0
      - name: VolumeBinding
        weight: 0
      - name: VolumeZone
        weight: 0
      - name: PodTopologySpread
        weight: 0
      - name: InterPodAffinity
        weight: 0
      - name: NodeResourceTopologyMatch
        weight: 0
    multiPoint: {}
    permit: {}
    postBind: {}
    postFilter:
      enabled:
      - name: DefaultPreemption
        weight: 0
    preBind:
      enabled:
      - name: VolumeBinding
        weight: 0
    preFilter:
      enabled:
      - name: NodeResourcesFit
        weight: 0
      - name: NodePorts
        weight: 0
      - name: VolumeRestrictions
        weight: 0
      - name: PodTopologySpread
        weight: 0
      - name: InterPodAffinity
        weight: 0
      - name: VolumeBinding
        weight: 0
      - name: NodeAffinity
        weight: 0
    preScore:
      enabled:
      - name: InterPodAffinity
        weight: 0
      - name: PodTopologySpread
        weight: 0
      - name: TaintToleration
        weight: 0
      - name: NodeAffinity
        weight: 0
    queueSort:
      enabled:
      - name: PrioritySort
        weight: 0
    reserve:
      enabled:
      - name: VolumeBinding
        weight: 0
      - name: NodeResourceTopologyMatch
        weight: 0
    score:
      enabled:
      - name: NodeResourcesBalancedAllocation
        weight: 1
      - name: ImageLocality
        weight: 1
      - name: InterPodAffinity
        weight: 1
      - name: NodeResourcesFit
        weight: 1
      - name: NodeAffinity
        weight: 1
      - name: PodTopologySpread
        weight: 2
      - name: TaintToleration
        weight: 1
      - name: NodeResourceTopologyMatch
        weight: 0
  schedulerName: test-topo-aware-sched
`
)
