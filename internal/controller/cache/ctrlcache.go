/*
 * Copyright 2025 Red Hat, Inc.
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

package controller

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectDeduper struct {
	lock sync.Mutex
	objs map[string]client.Object
}

func NewObjectDeduper(objs ...client.Object) *ObjectDeduper {
	od := ObjectDeduper{
		objs: make(map[string]client.Object),
	}
	for _, obj := range objs {
		gvk := keyFor(obj)
		od.objs[gvk] = obj
	}
	return &od
}

func (od *ObjectDeduper) UniquePtr(obj client.Object) client.Object {
	od.lock.Lock()
	defer od.lock.Unlock()
	gvk := keyFor(obj)
	if objKey, ok := od.objs[gvk]; ok {
		klog.V(4).InfoS("reusing object pointer", "gvk", gvk)
		return objKey
	}
	klog.V(4).InfoS("creating object pointer", "gvk", gvk)
	od.objs[gvk] = obj
	return obj
}

type ByObjectBuilder struct {
	od   *ObjectDeduper
	data map[client.Object]cache.ByObject
}

func MakeByObject(od *ObjectDeduper) *ByObjectBuilder {
	return &ByObjectBuilder{
		od:   od,
		data: make(map[client.Object]cache.ByObject),
	}
}

func (bob *ByObjectBuilder) For(obj client.Object, namespaces ...string) *ByObjectBuilder {
	nsConf := map[string]cache.Config{}
	for _, ns := range namespaces {
		nsConf[ns] = cache.Config{}
	}
	bob.data[bob.od.UniquePtr(obj)] = cache.ByObject{
		Namespaces: nsConf,
	}
	return bob
}

func (bob *ByObjectBuilder) Done() map[client.Object]cache.ByObject {
	for obj, conf := range bob.data {
		klog.V(2).InfoS("ByObject cache", "gvk", keyFor(obj), "namespaces", strings.Join(slices.Sorted(maps.Keys(conf.Namespaces)), ","))
	}
	return bob.data
}

func keyFor(obj client.Object) string {
	objType := reflect.TypeOf(obj).Elem()
	return objType.PkgPath() + "." + objType.Name()
}
