package nodepools

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"strings"
	"testing"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
)

var (
	scheme = k8sruntime.NewScheme()
)

func TestGetByClusterName(t *testing.T) {
	utilruntime.Must(hypershiftv1beta1.AddToScheme(scheme))

	ctx := context.TODO()

	// Mock data for NodePoolList
	nodePools := []hypershiftv1beta1.NodePool{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodepool1",
				Namespace: "default",
			},
			Spec: hypershiftv1beta1.NodePoolSpec{
				ClusterName: "cluster1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodepool2",
				Namespace: "default",
			},
			Spec: hypershiftv1beta1.NodePoolSpec{
				ClusterName: "cluster2",
			},
		},
	}

	testCases := []struct {
		name              string
		clusterName       string
		expectedError     string
		expectedObjectKey string
	}{
		{
			name:              "Successfully finds NodePool by cluster name",
			clusterName:       "cluster1",
			expectedError:     "",
			expectedObjectKey: client.ObjectKeyFromObject(&nodePools[0]).String(),
		},
		{
			name:              "Returns error when no NodePool matches cluster name",
			clusterName:       "nonexistent-cluster",
			expectedError:     "failed to find nodePool associated with cluster \"nonexistent-cluster\"; existing nodePools are:",
			expectedObjectKey: "",
		},
	}

	// Run testCases
	t.Run("GetByClusterName Tests", func(t *testing.T) {
		// Initialize a fake client with the mock data
		fakeClient := fake.NewClientBuilder().WithObjects(&nodePools[0], &nodePools[1]).WithScheme(scheme).Build()
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				np, err := GetByClusterName(ctx, fakeClient, tc.clusterName)
				if err != nil {
					if tc.expectedError == "" {
						t.Errorf("no error expected to be returned; got %v", err)
					}
					if !strings.Contains(err.Error(), tc.expectedError) {
						t.Errorf("expected error %v, got %v", tc.expectedError, err)
					}
				} else if tc.expectedObjectKey != client.ObjectKeyFromObject(np).String() {
					t.Errorf("expected object key %v, got %v", tc.expectedObjectKey, client.ObjectKeyFromObject(np).String())
				}
			})
		}
	})
}

func TestAddObjectRef(t *testing.T) {
	// Mock object
	mockObject := &corev1.ConfigMap{}
	mockObject.SetName("object1")

	testCases := []struct {
		name          string
		initialConfig []corev1.LocalObjectReference
		expected      []corev1.LocalObjectReference
	}{
		{
			name: "Add new object reference",
			initialConfig: []corev1.LocalObjectReference{
				{Name: "object2"},
				{Name: "object3"},
			},
			expected: []corev1.LocalObjectReference{
				{Name: "object1"},
				{Name: "object2"},
				{Name: "object3"},
			},
		},
		{
			name: "Replace existing object reference",
			initialConfig: []corev1.LocalObjectReference{
				{Name: "object1"},
				{Name: "object3"},
			},
			expected: []corev1.LocalObjectReference{
				{Name: "object1"},
				{Name: "object3"},
			},
		},
		{
			name:     "Add to empty configuration",
			expected: []corev1.LocalObjectReference{{Name: "object1"}},
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := addObjectRef(mockObject, tc.initialConfig)
			if len(result) != len(tc.expected) {
				t.Errorf("Test %q failed: expected length %d, got %d", tc.name, len(tc.expected), len(result))
			}
			for i := range result {
				if result[i].Name != tc.expected[i].Name {
					t.Errorf("Test %q failed: expected %v, got %v", tc.name, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestRemoveObjectRef(t *testing.T) {
	// Mock object
	mockObject := &corev1.ConfigMap{}
	mockObject.SetName("object1")

	testCases := []struct {
		name          string
		initialConfig []corev1.LocalObjectReference
		expected      []corev1.LocalObjectReference
	}{
		{
			name: "Remove existing object reference",
			initialConfig: []corev1.LocalObjectReference{
				{Name: "object1"},
				{Name: "object2"},
			},
			expected: []corev1.LocalObjectReference{
				{Name: "object2"},
			},
		},
		{
			name: "Do nothing if object reference does not exist",
			initialConfig: []corev1.LocalObjectReference{
				{Name: "object2"},
				{Name: "object3"},
			},
			expected: []corev1.LocalObjectReference{
				{Name: "object2"},
				{Name: "object3"},
			},
		},
		{
			name:     "Handle empty configuration",
			expected: []corev1.LocalObjectReference{},
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := removeObjectRef(mockObject, tc.initialConfig)
			if len(result) != len(tc.expected) {
				t.Errorf("Test %q failed: expected length %d, got %d", tc.name, len(tc.expected), len(result))
			}
			for i := range result {
				if result[i].Name != tc.expected[i].Name {
					t.Errorf("Test %q failed: expected %v, got %v", tc.name, tc.expected[i], result[i])
				}
			}
		})
	}
}
