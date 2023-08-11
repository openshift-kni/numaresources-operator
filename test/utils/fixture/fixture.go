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

package fixture

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/ginkgo/v2"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	"github.com/openshift-kni/numaresources-operator/internal/objects"
	intwait "github.com/openshift-kni/numaresources-operator/internal/wait"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Fixture struct {
	// Client defines the API client to run CRUD operations, that will be used for testing
	Client client.Client
	// K8sClient defines k8s client to run subresource operations, for example you should use it to get pod logs
	K8sClient      *kubernetes.Clientset
	Namespace      corev1.Namespace
	InitialNRTList nrtv1alpha2.NodeResourceTopologyList
	Skipped        bool
	avoidCooldown  bool
}

const (
	defaultTeardownTime      = 180 * time.Second
	defaultCooldownTime      = 30 * time.Second
	defaultSettleInterval    = 11 * time.Second
	defaultSettleTimeout     = 1 * time.Minute
	defaultCooldownThreshold = 5
)

type Options uint

const (
	OptionNone          = 0
	OptionRandomizeName = 1 << iota
	OptionAvoidCooldown = 2 << iota
)

var (
	teardownTime      time.Duration
	cooldownTime      time.Duration
	settleInterval    time.Duration
	settleTimeout     time.Duration
	cooldownThreshold int
)

func init() {
	teardownTime = getTimeDurationFromEnvVar("E2E_NROP_TEST_TEARDOWN", "teardown time", defaultTeardownTime)
	cooldownTime = getTimeDurationFromEnvVar("E2E_NROP_TEST_COOLDOWN", "cooldown time", defaultCooldownTime)
	settleInterval = getTimeDurationFromEnvVar("E2E_NROP_TEST_SETTLE_INTERVAL", "settle interval", defaultSettleInterval)
	settleTimeout = getTimeDurationFromEnvVar("E2E_NROP_TEST_SETTLE_TIMEOUT", "settle timeout", defaultSettleTimeout)
	cooldownThreshold = getCooldownThresholdFromEnvVar()
}

func SetupWithOptions(name string, nrtList nrtv1alpha2.NodeResourceTopologyList, options Options) (*Fixture, error) {
	if !e2eclient.ClientsEnabled {
		return nil, fmt.Errorf("clients not enabled")
	}
	randomizeName := (options & OptionRandomizeName) == OptionRandomizeName
	avoidCooldown := (options & OptionAvoidCooldown) == OptionAvoidCooldown
	ns, err := setupNamespace(e2eclient.Client, name, randomizeName)
	if err != nil {
		klog.Errorf("cannot setup namespace %q: %v", name, err)
		return nil, err
	}
	ginkgo.By(fmt.Sprintf("set up the test namespace %q", ns.Name))
	return &Fixture{
		Client:         e2eclient.Client,
		K8sClient:      e2eclient.K8sClient,
		Namespace:      ns,
		InitialNRTList: nrtList,
		avoidCooldown:  avoidCooldown,
	}, nil

}

func Setup(baseName string, nrtList nrtv1alpha2.NodeResourceTopologyList) (*Fixture, error) {
	return SetupWithOptions(baseName, nrtList, OptionRandomizeName)
}

func Teardown(ft *Fixture) error {
	ginkgo.By(fmt.Sprintf("tearing down the test namespace %q", ft.Namespace.Name))
	err := teardownNamespace(ft.Client, ft.Namespace)
	if err != nil {
		klog.Errorf("cannot teardown namespace %q: %s", ft.Namespace.Name, err)
		return err
	}

	if ft.Skipped {
		ft.Skipped = false
		ginkgo.By(fmt.Sprintf("skipped - nothing to cool down"))
		return nil
	}

	if ft.avoidCooldown {
		ginkgo.By(fmt.Sprintf("skipped - cool down disabled"))
		return nil
	}

	Cooldown(ft)
	return nil
}

func Skip(ft *Fixture, message string) {
	ft.Skipped = true
	ginkgo.Skip(message)
}

func Skipf(ft *Fixture, format string, args ...interface{}) {
	Skip(ft, fmt.Sprintf(format, args...))
}

func Cooldown(ft *Fixture) {
	if len(ft.InitialNRTList.Items) > 0 {
		interval := 5 * time.Second
		ginkgo.By(fmt.Sprintf("cooldown by verifying NRTs data is settled to the initial state (interval=%v timeout=%v)", interval, settleTimeout))
		currentNrtList, err := intwait.With(ft.Client).Interval(interval).Timeout(settleTimeout).ForNodeResourceTopologiesEqualTo(context.TODO(), &ft.InitialNRTList, intwait.NRTIgnoreNothing)
		if err != nil {
			klog.Warning("NRT MISMATCH:\n")
			a, _ := yaml.Marshal(ft.InitialNRTList.Items)
			klog.Infof("-----Initial NRT: \n%s\n", a)
			b, _ := yaml.Marshal(currentNrtList.Items)
			klog.Infof("-----Current NRT: \n%s\n", b)

			ginkgo.Fail("cooldown failed, the NRT data did not settle back to the initial state")
		}
		return
	}
	klog.Warningf("cooling down for %v", cooldownTime)
	time.Sleep(cooldownTime)
}

func WaitForNRTSettle(fxt *Fixture) (*nrtv1alpha2.NodeResourceTopologyList, error) {
	klog.Infof("cooldown by verifying NRTs data is settled (interval=%v timeout=%v)", settleInterval, settleTimeout)
	settledNRT, err := intwait.With(fxt.Client).Interval(settleInterval).Timeout(settleTimeout).ForNodeResourceTopologiesSettled(context.TODO(), cooldownThreshold, intwait.NRTIgnoreNothing)
	return &settledNRT, err
}

func setupNamespace(cli client.Client, baseName string, randomize bool) (corev1.Namespace, error) {
	name := baseName
	if randomize {
		// intentionally avoid GenerateName like the k8s e2e framework does
		name = RandomizeName(baseName)
	}
	ns := objects.NewNamespace(name)
	err := cli.Create(context.TODO(), ns)
	if err != nil {
		return *ns, err
	}

	// again we do like the k8s e2e framework does and we try to be robust
	var updatedNs corev1.Namespace
	err = wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		err := cli.Get(context.TODO(), client.ObjectKeyFromObject(ns), &updatedNs)
		if err != nil {
			return false, err
		}
		return true, nil
	})
	return updatedNs, err
}

func teardownNamespace(cli client.Client, ns corev1.Namespace) error {
	err := cli.Delete(context.TODO(), &ns)
	if apierrors.IsNotFound(err) {
		return nil
	}

	klog.Warningf("tearing down up to %v", teardownTime)

	iterations := 0
	updatedNs := corev1.Namespace{}
	return wait.PollImmediate(1*time.Second, teardownTime, func() (bool, error) {
		iterations++
		err := cli.Get(context.TODO(), client.ObjectKeyFromObject(&ns), &updatedNs)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		if iterations%10 == 0 {
			klog.InfoS("tearing down namespace: still not gone", "namespace", ns.Name, "error", err)
		}
		return false, nil
	})
}

func RandomizeName(baseName string) string {
	return fmt.Sprintf("%s-%s", baseName, strconv.Itoa(rand.Intn(10000)))
}

func getTimeDurationFromEnvVar(envVarName, description string, fallbackValue time.Duration) time.Duration {
	raw, ok := os.LookupEnv(envVarName)
	if !ok {
		return fallbackValue
	}
	val, err := time.ParseDuration(raw)
	if err != nil {
		klog.Errorf("cannot parse the provided test %s (fallback to default: %v): %v", description, fallbackValue, err)
		return fallbackValue
	}
	return val
}

func getCooldownThresholdFromEnvVar() int {
	raw, ok := os.LookupEnv("E2E_NROP_COOLDOWN_THRESHOLD")
	if !ok {
		return defaultCooldownThreshold
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		klog.Errorf("cannot parse the provided test cooldown threshold for resources (fallback to default: %v): %v", defaultCooldownThreshold, err)
		return defaultCooldownThreshold
	}
	return val
}
