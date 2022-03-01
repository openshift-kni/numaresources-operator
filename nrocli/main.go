/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"syscall"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"

	nrovalidator "github.com/openshift-kni/numaresources-operator/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
)

type ProgArgs struct {
	Version bool
	Verbose bool
	Quiet   bool
}

func main() {
	parsedArgs, err := parseArgs(os.Args[1:]...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse args: %v\n", err)
		//TODO: print usage
		os.Exit(int(syscall.EINVAL))
	}

	if parsedArgs.Version {
		fmt.Println(version.ProgramName(), version.Get())
		os.Exit(0)
	}

	if err := validateCluster(parsedArgs); err != nil {
		if !parsedArgs.Quiet {
			fmt.Fprintf(os.Stderr, "Error while trying to validate cluster: %v\n", err)
		}
		os.Exit(-1)
	}
}

func parseArgs(args ...string) (ProgArgs, error) {
	pArgs := ProgArgs{}

	flags := flag.NewFlagSet(version.ProgramName(), flag.ExitOnError)

	flags.BoolVar(&pArgs.Version, "version", false, "Output version and exit")
	flags.BoolVar(&pArgs.Verbose, "verbose", false, "Verbose output")
	flags.BoolVar(&pArgs.Quiet, "quiet", false, "Avoid all output. Has precende over 'verbose'")

	err := flags.Parse(args)
	if err != nil {
		return pArgs, err
	}

	if pArgs.Version {
		return pArgs, err
	}

	return pArgs, nil
}

func validateCluster(args ProgArgs) error {
	cli, err := clientutil.New()
	if err != nil {
		return err
	}

	data, err := nrovalidator.CollectData(context.TODO(), cli)
	if err != nil {
		return err
	}

	result, err := nrovalidator.Validate(*data)
	if err != nil {
		return err
	}

	if !args.Quiet {
		printValidationResults(result, args.Verbose)
	}
	return nil
}

func printValidationResults(items []deployervalidator.ValidationResult, verbose bool) {
	if len(items) == 0 {
		fmt.Printf("PASSED>>: cluster kubelet configuration looks ok!\n")
	} else {
		fmt.Printf("FAILED>>: cluster kubelet configuration does NOT look ok!\n")

		if verbose {
			for idx, item := range items {
				fmt.Fprintf(os.Stderr, "ERROR#%03d: %s\n", idx, item.String())
			}
		}
	}
}
