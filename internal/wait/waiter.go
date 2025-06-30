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
 * Copyright 2023 Red Hat, Inc.
 */

package wait

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultPollInterval = 1 * time.Second
	// DefaultPollTimeout was computed by trial and error, not scientifically,
	// so it may adjusted in the future any time.
	// Roughly match the time it takes for pods to go running in CI.
	DefaultPollTimeout = 3 * time.Minute
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
	PollTimeout  time.Duration
	PollInterval time.Duration
	PollSteps    int // alternative to Timeout
}

func With(cli client.Client) *Waiter {
	return &Waiter{
		Cli:          cli,
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

func (wt *Waiter) Steps(st int) *Waiter {
	wt.PollSteps = st
	return wt
}
