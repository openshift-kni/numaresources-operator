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

package wait

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

const (
	// found by trial and error, no hard math behind, can change anytime
	DefaultPollInterval = 2 * time.Second
	DefaultPollTimeout  = 2 * time.Minute
)

type ObjectKey struct {
	Namespace string
	Name      string
}

func ObjectKeyFromObject(obj metav1.Object) ObjectKey {
	return ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

func (ok ObjectKey) AsKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: ok.Namespace,
		Name:      ok.Name,
	}
}

func (ok ObjectKey) String() string {
	return fmt.Sprintf("%s/%s", ok.Namespace, ok.Name)
}

type Waiter struct {
	Cli          client.Client
	Log          logr.Logger
	PollTimeout  time.Duration
	PollInterval time.Duration
}

func With(cli client.Client, log logr.Logger) *Waiter {
	return &Waiter{
		Cli:          cli,
		Log:          log,
		PollTimeout:  DefaultPollTimeout,
		PollInterval: DefaultPollInterval,
	}
}

func (wt *Waiter) Timeout(tt time.Duration) *Waiter {
	wt.PollTimeout = tt
	return wt
}

func (wt *Waiter) Interval(iv time.Duration) *Waiter {
	wt.PollInterval = iv
	return wt
}

func (wt Waiter) ForNamespaceDeleted(ctx context.Context, namespace string) error {
	log := wt.Log.WithValues("namespace", namespace)
	log.Info("wait for the namespace to be gone")
	return k8swait.PollImmediate(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		nsKey := ObjectKey{Name: namespace}
		ns := corev1.Namespace{} // unused
		err := wt.Cli.Get(ctx, nsKey.AsKey(), &ns)
		return deletionStatusFromError(wt.Log, "Namespace", nsKey, err)
	})
}

func deletionStatusFromError(logger logr.Logger, kind string, key ObjectKey, err error) (bool, error) {
	if err == nil {
		logger.Info("object still present", "kind", kind, "key", key.String())
		return false, nil
	}
	if k8serrors.IsNotFound(err) {
		logger.Info("object is gone", "kind", kind, "key", key.String())
		return true, nil
	}
	logger.Info("failed to get object", "kind", kind, "key", key.String(), "error", err)
	return false, err
}
