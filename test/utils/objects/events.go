/*
Copyright 2022 The Kubernetes Authors.

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

package objects

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func LogEvents(k8sCli *kubernetes.Clientset, kind, namespace, name string) error {
	klog.Infof("checking events for pod %s/%s", namespace, name)
	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", name),
		TypeMeta:      metav1.TypeMeta{Kind: kind},
	}
	events, err := k8sCli.CoreV1().Events(namespace).List(context.TODO(), opts)
	if err != nil {
		klog.ErrorS(err, "cannot get events for %s %s/%s", kind, namespace, name)
		return err
	}
	klog.Infof("begin events for %s/%s", namespace, name)
	for _, item := range events.Items {
		klog.Infof("+- event: %s %s: %s %s", item.Type, item.ReportingController, item.Reason, item.Message)
	}
	klog.Infof("end events for %s/%s", namespace, name)
	return nil
}
