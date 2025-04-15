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

package mustgather

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/hypershift/consts"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
)

var _ = Describe("[must-gather] NRO data collected", func() {
	Context("with a freshly executed must-gather command", func() {
		var destDir string

		BeforeEach(func() {
			Expect(nroSchedObj).ToNot(BeNil(), "missing scheduler object reference")

			var err error
			destDir, err = os.MkdirTemp("", "*-e2e-data")
			Expect(err).ToNot(HaveOccurred())
			By(fmt.Sprintf("using destination data directory: %q", destDir))

			By("Looking for oc tool")
			ocExec, err := exec.LookPath("oc")
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "Unable to find oc executable: %v\n", err)
				Skip(fmt.Sprintf("unable to find 'oc' executable %v\n", err))
			}

			mgImageParam := fmt.Sprintf("--image=%s:%s", mustGatherImage, mustGatherTag)
			mgDestDirParam := fmt.Sprintf("--dest-dir=%s", destDir)

			cmdline := []string{
				ocExec,
				"adm",
				"must-gather",
				mgImageParam,
				mgDestDirParam,
			}
			By(fmt.Sprintf("running: %v\n", cmdline))

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter

			_, err = cmd.Output()
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			if _, ok := os.LookupEnv("E2E_NROP_MUSTGATHER_CLEANUP_SKIP"); ok {
				return
			}
			err := os.RemoveAll(destDir)
			if err != nil {
				klog.Warningf("unable to remove temporary directory %q: %v", destDir, err)
			}
		})

		It("check NRO data files have been collected", func(ctx context.Context) {
			crdDefinitions := []string{
				"cluster-scoped-resources/apiextensions.k8s.io/customresourcedefinitions/noderesourcetopologies.topology.node.k8s.io.yaml",
				"cluster-scoped-resources/apiextensions.k8s.io/customresourcedefinitions/numaresourcesoperators.nodetopology.openshift.io.yaml",
				"cluster-scoped-resources/apiextensions.k8s.io/customresourcedefinitions/numaresourcesschedulers.nodetopology.openshift.io.yaml",
			}

			destDirContent, err := os.ReadDir(destDir)
			Expect(err).NotTo(HaveOccurred(), "unable to read contents from destDir:%s. error: %w", destDir, err)

			for _, content := range destDirContent {
				if !content.IsDir() {
					continue
				}
				mgContentFolder := filepath.Join(destDir, content.Name())

				By(fmt.Sprintf("Checking Folder: %q", mgContentFolder))
				By("Looking for CRD definitions")
				err = checkfilesExist(crdDefinitions, mgContentFolder)
				Expect(err).ToNot(HaveOccurred())

				By("Looking for resources instances")
				nropInstanceFileName := fmt.Sprintf("%s.yaml", filepath.Join("cluster-scoped-resources/nodetopology.openshift.io/numaresourcesoperators", objects.NROObjectKey().Name))
				nroschedInstanceFileName := fmt.Sprintf("%s.yaml", filepath.Join("cluster-scoped-resources/nodetopology.openshift.io/numaresourcesschedulers", nroSchedObj.Name))

				nro := &nropv1.NUMAResourcesOperator{}
				ok, err := getResourceOfType(filepath.Join(mgContentFolder, "cluster-scoped-resources/nodetopology.openshift.io/numaresourcesoperators"), objects.NROObjectKey().Name, nro)
				Expect(ok).To(BeTrue(), "numaresourcesoperators resources named %s was not found", objects.NROObjectKey().Name)
				Expect(err).NotTo(HaveOccurred())

				var nglabels []map[string]string
				if configuration.Plat == platform.OpenShift {
					collectedMCPs, err := getMachineConfigPools(filepath.Join(mgContentFolder, "cluster-scoped-resources/machineconfiguration.openshift.io/machineconfigpools"))
					Expect(err).ToNot(HaveOccurred())
					ngMCPs, err := nodegroupv1.FindMachineConfigPools(&collectedMCPs, nro.Spec.NodeGroups)
					Expect(err).ToNot(HaveOccurred())
					nglabels = collectMachineConfigPoolsNodeSelector(ngMCPs)
				}
				if configuration.Plat == platform.HyperShift {
					for _, ng := range nro.Spec.NodeGroups {
						nglabels = append(nglabels, map[string]string{
							consts.NodePoolNameLabel: *ng.PoolName,
						})
					}
					By("Check that the ConfigMaps have been collected")
					configMapsFolder := filepath.Join(mgContentFolder, "namespaces/openshift-config-managed/core/configmaps")
					re := regexp.MustCompile(`^kubeletconfig-.*\.yaml`)
					found := checkfileExistByRegex(configMapsFolder, re)
					Expect(found).To(BeTrue(), "couldn't find a file starts with \"kubeletconfig-\" prefix in directory %q", configMapsFolder)
				}
				workerNodesNames, err := getWorkerNodesNames(filepath.Join(mgContentFolder, "cluster-scoped-resources/core/nodes"), nglabels)
				Expect(err).ToNot(HaveOccurred())

				crdInstances := []string{nropInstanceFileName, nroschedInstanceFileName}
				for _, value := range workerNodesNames {
					crdInstances = append(crdInstances, fmt.Sprintf("%s.yaml", filepath.Join("cluster-scoped-resources/topology.node.k8s.io/noderesourcetopologies", value)))
				}
				err = checkfilesExist(crdInstances, mgContentFolder)
				Expect(err).ToNot(HaveOccurred())

				By("Looking for namespace in NUMAResourcesOperator")
				updatedNRO, err := wait.With(e2eclient.Client).Interval(5*time.Second).Timeout(2*time.Minute).ForDaemonsetInNUMAResourcesOperatorStatus(ctx, nro)
				Expect(err).ToNot(HaveOccurred())
				namespace := updatedNRO.Status.DaemonSets[0].Namespace

				By(fmt.Sprintf("Checking: %q namespace\n", namespace))
				namespaceFolder := filepath.Join(mgContentFolder, "namespaces/", namespace)
				items := []string{
					"core/pods.yaml",
					"pods",
				}
				err = checkfilesExist(items, namespaceFolder)
				Expect(err).ToNot(HaveOccurred())

				podsFolder := filepath.Join(namespaceFolder, "pods")
				podsFolders, err := os.ReadDir(podsFolder)
				Expect(err).ToNot(HaveOccurred())

				podFolderNames := []string{}
				for _, podFolder := range podsFolders {
					if !podFolder.IsDir() {
						continue
					}
					podFolderNames = append(podFolderNames, podFolder.Name())
				}
				Expect(podFolderNames).To(ContainElement(MatchRegexp("^numaresources-controller-manager*")))
				Expect(podFolderNames).To(ContainElement(MatchRegexp("^secondary-scheduler*")))
			}
		})
	})
})

func collectMachineConfigPoolsNodeSelector(mcps []*mcov1.MachineConfigPool) []map[string]string {
	labelsGroup := []map[string]string{}
	for _, mcp := range mcps {
		labels := mcp.Spec.NodeSelector.MatchLabels
		labelsGroup = append(labelsGroup, labels)
	}
	return labelsGroup
}

func checkfilesExist(listOfFiles []string, path string) error {
	for _, f := range listOfFiles {
		file := filepath.Join(path, f)
		if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func checkfileExistByRegex(dirPath string, regexp *regexp.Regexp) bool {
	GinkgoHelper()
	dirEntry, err := os.ReadDir(dirPath)
	Expect(err).ToNot(HaveOccurred())

	for _, entry := range dirEntry {
		if entry.IsDir() {
			continue
		}
		if regexp.MatchString(entry.Name()) {
			return true
		}
	}
	return false
}

// Look for node yaml manifest in `folder` and return
// all the names of all the nodes with
// any set of labels
func getWorkerNodesNames(folder string, labelsGroups []map[string]string) ([]string, error) {
	retval := []string{}
	items, err := os.ReadDir(folder)
	if err != nil {
		return retval, err
	}

	for _, item := range items {
		if item.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(folder, item.Name()))
		if err != nil {
			return retval, err
		}

		node := &corev1.Node{}
		err = yaml.Unmarshal(data, node)
		if err != nil {
			return retval, err
		}

		for _, labels := range labelsGroups {
			if isMapSubsetOf(node.ObjectMeta.Labels, labels) {
				retval = append(retval, node.Name)
				break
			}
		}
	}
	return retval, nil
}

func getMachineConfigPools(folder string) (mcov1.MachineConfigPoolList, error) {
	retval := mcov1.MachineConfigPoolList{}
	items, err := os.ReadDir(folder)
	if err != nil {
		return retval, err
	}

	for _, item := range items {
		if item.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(folder, item.Name()))
		if err != nil {
			return retval, err
		}

		mcp := &mcov1.MachineConfigPool{}
		err = yaml.Unmarshal(data, mcp)
		if err != nil {
			return retval, err
		}

		retval.Items = append(retval.Items, *mcp)
	}
	return retval, nil
}

func getResourceOfType(folder, resourceName string, resource any) (bool, error) {
	items, err := os.ReadDir(folder)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		if item.Name() != fmt.Sprintf("%s.yaml", resourceName) {
			continue
		}

		data, err := os.ReadFile(filepath.Join(folder, item.Name()))
		if err != nil {
			return false, err
		}
		return true, yaml.Unmarshal(data, resource)
	}
	return false, nil
}

// return true if b keys and values respectively are part of a, otherwise false
func isMapSubsetOf(a map[string]string, b map[string]string) bool {
	if len(a) < len(b) || len(a) == 0 {
		return false
	}

	for k, bv := range b {
		av, ok := a[k]

		if !ok || av != bv {
			return false
		}
	}
	return true
}
