/*
 * Copyright 2024 Red Hat, Inc.
 *
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
 */

package workarounds

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestUpdateConfigMap(t *testing.T) {
	expectedConfig := `apiVersion: v1
data:
  config.yaml: |
    prometheusK8s:
      volumeClaimTemplate: {}
    telemeterClient:
      enabled: false
kind: ConfigMap
metadata:
  creationTimestamp: null
  name: cluster-monitoring-config
  namespace: openshift-monitoring
`
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: promConfigMapNamespace,
			Name:      promConfigMapName,
		},
		Data: map[string]string{
			promConfigMapKey: `telemeterClient:
  enabled: false
prometheusK8s:
  volumeClaimTemplate:
    metadata:
      name: prometheus-data
      annotations:
        openshift.io/cluster-monitoring-drop-pvc: "yes"
    spec:
      resources:
        requests:
          storage: 20Gi`,
		},
	}

	err := UpdateConfigMap(&cm)
	if err != nil {
		t.Fatalf("UpdateConfigData failed: %v", err)
	}

	yamlSerializer := serializer.NewSerializerWithOptions(
		serializer.DefaultMetaFactory, scheme.Scheme, scheme.Scheme,
		serializer.SerializerOptions{Yaml: true, Pretty: true, Strict: true})

	buff := &bytes.Buffer{}
	if err := yamlSerializer.Encode(&cm, buff); err != nil {
		// supervised testing environment
		// should never be happening
		panic(fmt.Errorf("failed to encode  KubeletConfigConfigMap object %w", err))
	}

	fixedConfig := buff.String()

	if diff := cmp.Diff(fixedConfig, expectedConfig); diff != "" {
		t.Errorf("unexpected diff: %v", diff)
	}
}

// example from real cluster
const _ = `apiVersion: v1
data:
  config.yaml: |-
    telemeterClient:
      enabled: false
    prometheusK8s:
      volumeClaimTemplate:
        metadata:
          name: prometheus-data
          annotations:
            openshift.io/cluster-monitoring-drop-pvc: "yes"
        spec:
          resources:
            requests:
              storage: 20Gi
kind: ConfigMap
metadata:
  creationTimestamp: "2024-12-05T08:57:33Z"
  name: cluster-monitoring-config
  namespace: openshift-monitoring
  resourceVersion: "1748"
  uid: fa0b71d1-6dd7-41da-8d75-807435523a02`
