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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

var _ = ginkgo.Describe("[must-gather] NRO data collected", func() {
	ginkgo.Context("with a freshly executed must-gather command", func() {
		var destDir string

		ginkgo.BeforeEach(func() {
			var err error
			destDir, err = os.MkdirTemp("", "*-e2e-data")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			ginkgo.By(fmt.Sprintf("using destination data directory: %q", destDir))

			ginkgo.By("Looking for oc tool")
			ocExec, err := exec.LookPath("oc")
			if err != nil {
				fmt.Fprintf(ginkgo.GinkgoWriter, "Unable to find oc executable: %v\n", err)
				ginkgo.Skip(fmt.Sprintf("unable to find 'oc' executable %v\n", err))
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
			ginkgo.By(fmt.Sprintf("running: %v\n", cmdline))

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = ginkgo.GinkgoWriter

			_, err = cmd.Output()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			if _, ok := os.LookupEnv("E2E_NROP_MUSTGATHER_CLEANUP_SKIP"); ok {
				return
			}
			os.RemoveAll(destDir)
		})

		ginkgo.It("check NRO data files have been collected", func(ctx context.Context) {
			crdDefinitions := []string{
				"cluster-scoped-resources/apiextensions.k8s.io/customresourcedefinitions/noderesourcetopologies.topology.node.k8s.io.yaml",
				"cluster-scoped-resources/apiextensions.k8s.io/customresourcedefinitions/numaresourcesoperators.nodetopology.openshift.io.yaml",
				"cluster-scoped-resources/apiextensions.k8s.io/customresourcedefinitions/numaresourcesschedulers.nodetopology.openshift.io.yaml",
			}

			destDirContent, err := os.ReadDir(destDir)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "unable to read contents from destDir:%s. error: %w", destDir, err)

			for _, content := range destDirContent {
				if !content.IsDir() {
					continue
				}
				mgContentFolder := filepath.Join(destDir, content.Name())

				ginkgo.By(fmt.Sprintf("Checking Folder: %q", mgContentFolder))
				ginkgo.By("Looking for CRD definitions")
				err = checkfilesExist(crdDefinitions, mgContentFolder)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				ginkgo.By("Looking for resources instances")
				nropInstanceFileName := fmt.Sprintf("%s.yaml", filepath.Join("cluster-scoped-resources/nodetopology.openshift.io/numaresourcesoperators", deployment.NroObj.Name))
				nroschedInstanceFileName := fmt.Sprintf("%s.yaml", filepath.Join("cluster-scoped-resources/nodetopology.openshift.io/numaresourcesschedulers", deployment.NroSchedObj.Name))

				collectedMCPs, err := getMachineConfigPools(filepath.Join(mgContentFolder, "cluster-scoped-resources/machineconfiguration.openshift.io/machineconfigpools"))
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				ngMCPs, err := nodegroupv1.FindMachineConfigPools(&collectedMCPs, deployment.NroObj.Spec.NodeGroups)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				nglabels := collectMachineConfigPoolsNodeSelector(ngMCPs)
				workerNodesNames, err := getWorkerNodesNames(filepath.Join(mgContentFolder, "cluster-scoped-resources/core/nodes"), nglabels)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				crdInstances := []string{nropInstanceFileName, nroschedInstanceFileName}
				for _, value := range workerNodesNames {
					crdInstances = append(crdInstances, fmt.Sprintf("%s.yaml", filepath.Join("cluster-scoped-resources/topology.node.k8s.io/noderesourcetopologies", value)))
				}
				err = checkfilesExist(crdInstances, mgContentFolder)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				ginkgo.By("Looking for namespace in NUMAResourcesOperator")
				updatedNRO, err := wait.With(e2eclient.Client).Interval(5*time.Second).Timeout(2*time.Minute).ForDaemonsetInNUMAResourcesOperatorStatus(ctx, deployment.NroObj)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				namespace := updatedNRO.Status.DaemonSets[0].Namespace

				ginkgo.By(fmt.Sprintf("Checking: %q namespace\n", namespace))
				namespaceFolder := filepath.Join(mgContentFolder, "namespaces/", namespace)
				items := []string{
					"core/pods.yaml",
					"pods",
				}
				err = checkfilesExist(items, namespaceFolder)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				podsFolder := filepath.Join(namespaceFolder, "pods")
				podsFolders, err := os.ReadDir(podsFolder)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				podFolderNames := []string{}
				for _, podFolder := range podsFolders {
					if !podFolder.IsDir() {
						continue
					}
					podFolderNames = append(podFolderNames, podFolder.Name())
				}
				gomega.Expect(podFolderNames).To(gomega.ContainElement(gomega.MatchRegexp("^numaresources-controller-manager*")))
				gomega.Expect(podFolderNames).To(gomega.ContainElement(gomega.MatchRegexp("^secondary-scheduler*")))
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

// return true if B keys and values respectively are part of A, otherwise false
func isMapSubsetOf(A map[string]string, B map[string]string) bool {
	if len(A) < len(B) || len(A) == 0 {
		return false
	}

	for k, bv := range B {
		av, ok := A[k]

		if !ok || av != bv {
			return false
		}
	}
	return true
}
