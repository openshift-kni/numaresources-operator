/*
 * Copyright 2023 Red Hat, Inc.
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

package wait

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (wt Waiter) ForJobCompleted(ctx context.Context, jobNamespace, jobName string) (*batchv1.Job, error) {
	jobKey := client.ObjectKey{
		Namespace: jobNamespace,
		Name:      jobName,
	}
	updatedJob := batchv1.Job{}
	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		err := wt.Cli.Get(aContext, jobKey, &updatedJob)
		if err != nil {
			return false, err
		}
		if !isJobCompleted(updatedJob) {
			klog.Infof("%s/%s not yet completed (succeeded=%d)", jobNamespace, jobName, updatedJob.Status.Succeeded)
			return false, nil
		}
		klog.Infof("%s/%s completed! (succeeded=%d)", jobNamespace, jobName, updatedJob.Status.Succeeded)
		return true, nil

	})
	return &updatedJob, err
}

func isJobCompleted(job batchv1.Job) bool {
	cond := findJobCondition(job.Status.Conditions, batchv1.JobComplete)
	if cond == nil {
		return false
	}
	return cond.Status == corev1.ConditionTrue
}

func findJobCondition(conditions []batchv1.JobCondition, condType batchv1.JobConditionType) *batchv1.JobCondition {
	for idx := 0; idx < len(conditions); idx++ {
		cond := &conditions[idx]
		if cond.Type == condType {
			return cond
		}
	}
	return nil
}
