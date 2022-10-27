/*
 * Copyright 2022 Red Hat, Inc.
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

package podresfilter

import (
	"context"
	"path/filepath"

	"google.golang.org/grpc"

	"k8s.io/klog/v2"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"
)

type filteringClient struct {
	debug bool
	cli   podresourcesapi.PodResourcesListerClient
	// namespace glob -> name glob
	podExcludes map[string]string
}

func (fc *filteringClient) FilterListResponse(resp *podresourcesapi.ListPodResourcesResponse) *podresourcesapi.ListPodResourcesResponse {
	retResp := podresourcesapi.ListPodResourcesResponse{
		PodResources: make([]*podresourcesapi.PodResources, 0, len(resp.GetPodResources())),
	}
	for _, podRes := range resp.GetPodResources() {
		if ShouldExclude(fc.podExcludes, podRes.GetNamespace(), podRes.GetName(), fc.debug) {
			continue
		}
		retResp.PodResources = append(retResp.PodResources, podRes)
	}
	return &retResp
}

func (fc *filteringClient) FilterAllocatableResponse(resp *podresourcesapi.AllocatableResourcesResponse) *podresourcesapi.AllocatableResourcesResponse {
	return resp // nothing to do here
}

func (fc *filteringClient) List(ctx context.Context, in *podresourcesapi.ListPodResourcesRequest, opts ...grpc.CallOption) (*podresourcesapi.ListPodResourcesResponse, error) {
	resp, err := fc.cli.List(ctx, in, opts...)
	if err != nil {
		return resp, err
	}
	return fc.FilterListResponse(resp), nil
}

func (fc *filteringClient) GetAllocatableResources(ctx context.Context, in *podresourcesapi.AllocatableResourcesRequest, opts ...grpc.CallOption) (*podresourcesapi.AllocatableResourcesResponse, error) {
	resp, err := fc.cli.GetAllocatableResources(ctx, in, opts...)
	if err != nil {
		return resp, err
	}
	return fc.FilterAllocatableResponse(resp), nil
}

func NewFromLister(cli podresourcesapi.PodResourcesListerClient, debug bool, podExcludes map[string]string) podresourcesapi.PodResourcesListerClient {
	for namespaceGlob, nameGlob := range podExcludes {
		klog.Infof("> POD exclude: %s/%s", namespaceGlob, nameGlob)
	}
	return &filteringClient{
		debug:       debug,
		cli:         cli,
		podExcludes: podExcludes,
	}
}

func ShouldExclude(podExcludes map[string]string, namespace, name string, debug bool) bool {
	for namespaceGlob, nameGlob := range podExcludes {
		nsMatch, err := filepath.Match(namespaceGlob, namespace)
		if err != nil && debug {
			klog.Warningf("match error: namespace glob=%q pod=%s/%: %v", namespaceGlob, namespace, name, err)
			continue
		}
		if !nsMatch {
			continue
		}
		nMatch, err := filepath.Match(nameGlob, name)
		if err != nil && debug {
			klog.Warningf("match error: name glob=%q pod=%s/%: %v", namespaceGlob, namespace, name, err)
			continue
		}
		if !nMatch {
			continue
		}
		return true
	}
	return false
}
