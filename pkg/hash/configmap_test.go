/*
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2022 Red Hat, Inc.
 */

package hash

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift-kni/numaresources-operator/internal/objects"

	"github.com/asaskevich/govalidator"
)

func TestComputeCurrentConfigMap(t *testing.T) {
	testCases := []struct {
		name        string
		cli         client.Client
		cm          *corev1.ConfigMap
		expectedOut string
		expectedErr bool
	}{
		{
			name: "map not created",
			cli:  fake.NewFakeClient(),
			cm:   objects.NewKubeletConfigConfigMap("test", map[string]string{}, objects.NewKubeletConfigAutoresizeControlPlane()),
			// verified manually
			expectedOut: "SHA256:93909e569a15b6e4a5eefdcac4153f2c8179bf155143d10dac589f62ddcdf742",
			expectedErr: false,
		},
	}

	for _, tc := range testCases {
		out, err := ComputeCurrentConfigMap(context.TODO(), tc.cli, tc.cm.DeepCopy())
		gotErr := (err != nil)
		if gotErr != tc.expectedErr {
			t.Fatalf("got error %v expected error=%v", err, tc.expectedErr)
		}
		if out != tc.expectedOut {
			t.Fatalf("got output %q expected %q", out, tc.expectedOut)
		}
	}
}

func TestConfigMapData(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
	}
	testCases := []struct {
		testName string
		binData  map[string][]byte
		data     map[string]string
	}{
		{
			testName: "test1",
			binData: map[string][]byte{
				"foo1": []byte("bar1"),
				"foo2": []byte("bar2"),
			},
			data: map[string]string{
				"go": "lang",
			},
		},
		{
			testName: "test2",
			binData:  nil,
			data:     nil,
		},
	}

	for _, tc := range testCases {
		cm.Data = tc.data
		cm.BinaryData = tc.binData
		hash := ConfigMapData(cm)

		if hash == "" {
			t.Errorf("test: %q hash string cannot be empty", tc.testName)
		}

		if !govalidator.IsHash(strings.TrimLeft(hash, "SHA256:"), "sha256") {
			t.Errorf("test: %q ilegal hash string %q", tc.testName, hash)
		}
	}
}

// verify that different hashes are produced
// only when non metadata filed are changing
func TestConfigMapDataConsistency(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
	}

	hashedCms := make(map[string]string)
	type cmFileds struct {
		cmName  string
		labels  map[string]string
		binData map[string][]byte
		data    map[string]string
	}

	type testCase struct {
		cmfs                    []cmFileds
		cmfToCompare            string
		equalsCmHashesNames     []string
		noneEqualsCmHashesNames []string
	}

	testCases := []testCase{
		{
			cmfs: []cmFileds{
				{
					cmName: "name1",
					binData: map[string][]byte{
						"foo1": []byte("bar1"),
						"foo2": []byte("bar2"),
					},
					data: map[string]string{
						"go": "lang",
					},
				},
				{
					cmName: "name2",
					binData: map[string][]byte{
						"foo1": []byte("bar1"),
					},
					data: map[string]string{
						"go": "lang",
					},
				},
				{
					cmName: "name3",
					binData: map[string][]byte{
						"foo1": []byte("bar1"),
						"foo2": []byte("bar2"),
					},
					data: map[string]string{"go": "lang"},
				},
				{
					cmName:  "name4",
					binData: nil,
					data:    nil,
				},
			},
			cmfToCompare:            "name1",
			equalsCmHashesNames:     []string{"name3"},
			noneEqualsCmHashesNames: []string{"name2", "name4"},
		},
		{
			cmfs: []cmFileds{
				{
					cmName: "name1",
					labels: map[string]string{
						"label1": "",
					},
					data: map[string]string{
						"go": "lang",
					},
				},
				{
					cmName: "name2",
					labels: map[string]string{
						"different": "label.values",
					},
					data: map[string]string{
						"go": "lang",
					},
				},
			},
			cmfToCompare:            "name2",
			equalsCmHashesNames:     []string{"name2"},
			noneEqualsCmHashesNames: []string{},
		},
	}

	for _, tc := range testCases {
		for _, cmf := range tc.cmfs {
			cm.Labels = cmf.labels
			cm.Data = cmf.data
			cm.BinaryData = cmf.binData
			hashedCms[cmf.cmName] = ConfigMapData(cm)
		}

		for _, name := range tc.equalsCmHashesNames {
			if hashedCms[name] != hashedCms[tc.cmfToCompare] {
				t.Errorf("hashes of cm: %q and cm: %q are not equals", name, tc.cmfToCompare)
			}
		}

		for _, name := range tc.noneEqualsCmHashesNames {
			if hashedCms[name] == hashedCms[tc.cmfToCompare] {
				t.Errorf("hashes of cm: %q and cm: %q are equals when they shouldn't", name, tc.cmfToCompare)
			}
		}
	}
}
