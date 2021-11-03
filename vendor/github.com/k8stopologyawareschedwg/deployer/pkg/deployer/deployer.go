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
 * Copyright 2021 Red Hat, Inc.
 */

package deployer

import (
	"context"
	"regexp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
)

type WaitableObject struct {
	Obj  client.Object
	Wait func() error
}

type Helper struct {
	tag string
	cli client.Client
	log tlog.Logger
}

func NewHelper(tag string, log tlog.Logger) (*Helper, error) {
	cli, err := clientutil.New()
	if err != nil {
		return nil, err
	}
	return NewHelperWithClient(cli, tag, log), nil
}

func NewHelperWithClient(cli client.Client, tag string, log tlog.Logger) *Helper {
	return &Helper{
		tag: tag,
		cli: cli,
		log: log,
	}
}

func (hp *Helper) CreateObject(obj client.Object) error {
	objKind := obj.GetObjectKind().GroupVersionKind().Kind // shortcut
	if err := hp.cli.Create(context.TODO(), obj); err != nil {
		hp.log.Printf("-%5s> error creating %s %q: %v", hp.tag, objKind, obj.GetName(), err)
		return err
	}
	hp.log.Printf("-%5s> created %s %q", hp.tag, objKind, obj.GetName())
	return nil
}

func (hp *Helper) DeleteObject(obj client.Object) error {
	objKind := obj.GetObjectKind().GroupVersionKind().Kind // shortcut
	if err := hp.cli.Delete(context.TODO(), obj); err != nil {
		hp.log.Printf("-%5s> error deleting %s %q: %v", hp.tag, objKind, obj.GetName(), err)
		return err
	}
	hp.log.Printf("-%5s> deleted %s %q", hp.tag, objKind, obj.GetName())
	return nil
}

func (hp *Helper) GetObject(key client.ObjectKey, obj client.Object) error {
	return hp.cli.Get(context.TODO(), key, obj)
}

func (hp *Helper) GetPodsByPattern(namespace, pattern string) ([]*corev1.Pod, error) {
	var podList corev1.PodList
	err := hp.cli.List(context.TODO(), &podList)
	if err != nil {
		return nil, err
	}
	hp.log.Debugf("found %d pods in namespace %q matching pattern %q", len(podList.Items), namespace, pattern)

	podNameRgx, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	ret := []*corev1.Pod{}
	for _, pod := range podList.Items {
		if match := podNameRgx.FindString(pod.Name); len(match) != 0 {
			hp.log.Debugf("pod %q matches", pod.Name)
			ret = append(ret, &pod)
		}
	}
	return ret, nil
}

func (hp *Helper) GetDaemonSetByName(namespace, name string) (*appsv1.DaemonSet, error) {
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}
	var ds appsv1.DaemonSet
	err := hp.GetObject(key, &ds)
	if err != nil {
		return nil, err
	}
	return &ds, nil
}

func (hp *Helper) IsDaemonSetRunning(namespace, name string) (bool, error) {
	ds, err := hp.GetDaemonSetByName(namespace, name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			hp.log.Printf("daemonset %q %q not found - retrying", namespace, name)
			return false, nil
		}
		return false, err
	}
	hp.log.Printf("daemonset %q %q desired %d scheduled %d ready %d", namespace, name, ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled, ds.Status.NumberReady)
	return (ds.Status.DesiredNumberScheduled > 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberReady), nil
}

func (hp *Helper) IsDaemonSetGone(namespace, name string) (bool, error) {
	ds, err := hp.GetDaemonSetByName(namespace, name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			hp.log.Printf("daemonset %q %q not found - gone away!", namespace, name)
			return true, nil
		}
		return true, err
	}
	hp.log.Printf("daemonset %q %q running count %d", namespace, name, ds.Status.CurrentNumberScheduled)
	return false, nil
}
