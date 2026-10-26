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
	"fmt"
	"time"

	"github.com/go-logr/logr"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// found by trial and error, no hard math behind, can change anytime
	DefaultPollInterval = 2 * time.Second
	DefaultPollTimeout  = 2 * time.Minute
)

var (
	basePollInterval = DefaultPollInterval
	basePollTimeout  = DefaultPollTimeout
)

type ObjectKey struct {
	Namespace string
	Name      string
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
		PollTimeout:  basePollTimeout,
		PollInterval: basePollInterval,
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
