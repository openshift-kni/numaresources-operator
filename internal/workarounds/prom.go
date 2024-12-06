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
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	promConfigMapNamespace = "openshift-monitoring"
	promConfigMapName      = "cluster-monitoring-config"
	promConfigMapKey       = "config.yaml"

	promConfigKey       = "prometheusK8s"
	promConfigVolumeKey = "volumeClaimTemplate"
)

type Prometheus struct {
	PollInterval time.Duration
	PollTimeout  time.Duration
}

func ForPrometheus() *Prometheus {
	return &Prometheus{
		PollInterval: 5 * time.Second,
		PollTimeout:  5 * time.Minute,
	}
}

func (prom *Prometheus) IssueReference() string {
	return "https://github.com/kubernetes-sigs/aws-ebs-csi-driver/issues/1784"
}

func (prom *Prometheus) Describe() string {
	return "Prometheus instance  cannot use storage and fails to go running; only one instance out of two will go running, and this later prevents node draining"
}

func (prom *Prometheus) Apply(ctx context.Context, cli client.Client) error {
	var cm corev1.ConfigMap
	key := client.ObjectKey{
		Namespace: promConfigMapNamespace,
		Name:      promConfigMapName,
	}
	immediate := true
	return k8swait.PollUntilContextTimeout(ctx, prom.PollInterval, prom.PollTimeout, immediate, func(ctx2 context.Context) (bool, error) {
		err := cli.Get(ctx2, key, &cm)
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.InfoS("configmap not found, nothing to do", "namespace", key.Namespace, "name", key.Name)
				return true, nil
			}
			return true, err
		}

		err = UpdateConfigMap(&cm)
		if err != nil {
			return true, err
		}

		err = cli.Update(ctx2, &cm)
		return (err == nil), nil // retry on conflicts
	})
}

func UpdateConfigMap(cm *corev1.ConfigMap) error {
	data, ok := cm.Data[promConfigMapKey]
	if !ok {
		klog.InfoS("configmap holds no config data, nothing to do")
		return nil
	}

	klog.InfoS("configmap payload fixed", "oldData", data)

	newData, err := UpdateConfigData([]byte(data))
	if err != nil {
		klog.ErrorS(err, "configmap payload update failed")
		return err
	}

	klog.InfoS("configmap payload fixed", "newData", string(newData))

	cm.Data[promConfigMapKey] = string(newData)
	return nil
}

func UpdateConfigData(data []byte) ([]byte, error) {
	var err error
	var r unstructured.Unstructured

	err = yaml.Unmarshal(data, &r.Object)
	if err != nil {
		return nil, err
	}

	promK8S, ok, err := unstructured.NestedMap(r.Object, promConfigKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		klog.InfoS("configmap payload not found, nothing to do", "payload", promConfigKey)
		return nil, nil
	}

	err = unstructured.SetNestedMap(promK8S, map[string]interface{}{}, promConfigVolumeKey)
	if err != nil {
		klog.ErrorS(err, "configmap payload update failed", "namespace", "payload", promConfigKey, "volume", promConfigVolumeKey)
	}

	err = unstructured.SetNestedMap(r.Object, promK8S, promConfigKey)
	if err != nil {
		klog.ErrorS(err, "configmap payload update failed", "namespace", "payload", promConfigKey)
	}

	return yaml.Marshal(&r.Object)
}
