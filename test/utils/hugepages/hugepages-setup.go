package hugepages

import (
	"context"

	"k8s.io/utils/pointer"

	//hugepagesmc "github.com/openshift-kni/performance-addon-operators/tools/hugepages-machineconfig-generator"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"

	performancev2 "github.com/openshift-kni/performance-addon-operators/api/v2"
	testlog "github.com/openshift-kni/performance-addon-operators/functests/utils/log"
	"github.com/openshift-kni/performance-addon-operators/pkg/utils/hugepages"
)

func GetProfileWithHugepages() *performancev2.PerformanceProfile {
	// reserved := performancev2.CPUSet("0")
	// isolated := performancev2.CPUSet("1-3")
	hugePagesSize := performancev2.HugePageSize("1G")

	profile := &performancev2.PerformanceProfile{
		TypeMeta: machineconfigv1.TypeMeta{
			Kind:       "PerformanceProfile",
			APIVersion: performancev2.GroupVersion.String(),
		},
		ObjectMeta: machineconfigv1.ObjectMeta{
			Name: "performance",
		},
		Spec: performancev2.PerformanceProfileSpec{
			// CPU: &performancev2.CPU{
			// 	Reserved: &reserved,
			// 	Isolated: &isolated,
			// },
			HugePages: &performancev2.HugePages{
				DefaultHugePagesSize: &hugePagesSize,
				Pages: []performancev2.HugePage{
					{
						Size:  "1G",
						Count: 2,
						Node:  pointer.Int32Ptr(0),
					},
					{
						Size:  "2M",
						Count: 256,
						Node:  pointer.Int32Ptr(1),
					},
					{
						Size:  "1G",
						Count: 4,
						Node:  pointer.Int32Ptr(0),
					},
					{
						Size:  "2M",
						Count: 512,
						Node:  pointer.Int32Ptr(1),
					},
				},
			},
		},
	}
	return profile
}

func GetHugepagesMachineConfig(profile *performancev2.PerformanceProfile) (*machineconfigv1.MachineConfig, error) {
	//returns mc for hugepages
	mc, err := hugepages.MakeMachineConfig(profile.Spec.HugePages, "worker")
	if err != nil {
		return nil, err
	}
	return mc, nil
}

func ApplyMachineConfig(fxt *e2efixture.Fixture, mc *machineconfigv1.MachineConfig, mcp *machineconfigv1.MachineConfigPool) error {
	//creates the mc on the cluster
	err := fxt.Client.Create(context.TODO(), mc)
	if err != nil {
		testlog.Errorf("could not create machine config: %v", err)
	}

	//wait for mcp to start updating
	err = e2ewait.ForMachineConfigPoolCondition(fxt.Client, mcp, machineconfigv1.MachineConfigPoolUpdating, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
	if err != nil {
		testlog.Errorf("mcp did not start updating: %v", err)
	}
	//wait for mcp to be updated
	err = e2ewait.ForMachineConfigPoolCondition(fxt.Client, mcp, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
	if err != nil {
		testlog.Errorf("mcp is not yet updated: %v", err)
	}
	return nil
}

// func runBuild() {
// 	cmd := exec.Command("go", "build", "./main.go")
// 	err := cmd.Run()
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	//fmt.Print(string(hugepagesmc))
// }
