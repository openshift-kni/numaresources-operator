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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1"

	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"

	nrovalidator "github.com/openshift-kni/numaresources-operator/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(nropv1alpha1.AddToScheme(scheme))
	utilruntime.Must(machineconfigv1.Install(scheme))
	utilruntime.Must(securityv1.Install(scheme))
}

type ProgArgs struct {
	Version     bool
	Verbose     bool
	Quiet       bool
	JSON        bool
	Labels      string
	Validations sets.String
}

func main() {
	parsedArgs, err := parseArgs(os.Args[1:]...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse args: %v\n", err)
		os.Exit(1)
	}

	if parsedArgs.Version {
		fmt.Println(version.ProgramName(), version.Get())
		os.Exit(0)
	}

	if len(parsedArgs.Validations) == 0 || parsedArgs.Validations.Has("help") {
		fmt.Fprintf(os.Stderr, "available validators: %v\n", strings.Join(nrovalidator.Available().List(), ","))
		os.Exit(0)
	}

	err = validateCluster(parsedArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while trying to validate cluster: %v\n", err)
		os.Exit(2)
	}
}

func parseArgs(args ...string) (ProgArgs, error) {
	pArgs := ProgArgs{}

	var validationsArg string

	flags := flag.NewFlagSet(version.ProgramName(), flag.ExitOnError)

	flags.BoolVar(&pArgs.Version, "version", false, "Output version and exit")
	flags.BoolVar(&pArgs.Verbose, "verbose", false, "Verbose output")
	flags.BoolVar(&pArgs.JSON, "json", false, "Output JSON, not free text")
	flags.BoolVar(&pArgs.Quiet, "quiet", false, "Avoid all output. Overrides 'verbose'")
	flags.StringVar(&validationsArg, "what", "all", "Validations to perform")
	flags.StringVar(&pArgs.Labels, "labels", "", "Selector (label query) to filter on. e.g. -l key1=value1,key2=value2; autodetect with NRO if missing.")

	err := flags.Parse(args)
	if err != nil {
		return pArgs, err
	}

	pArgs.Validations, err = nrovalidator.Requested(validationsArg)
	return pArgs, err
}

func validateCluster(args ProgArgs) error {
	cli, err := NewClientWithScheme(scheme)
	if err != nil {
		return err
	}

	if !args.Quiet {
		fmt.Fprintf(os.Stderr, "INFO>>>>: enabled validators: %s\n", strings.Join(args.Validations.List(), ","))
	}

	data, err := nrovalidator.Collect(context.TODO(), cli, args.Labels, args.Validations)
	if err != nil {
		return err
	}

	rep, err := nrovalidator.Validate(data)
	if err != nil {
		return err
	}

	if args.Quiet {
		return nil
	}

	if !args.JSON {
		printValidationResults(rep.Errors, args.Verbose)
		return nil
	}

	return json.NewEncoder(os.Stdout).Encode(rep)
}

func printValidationResults(items []deployervalidator.ValidationResult, verbose bool) {
	if len(items) == 0 {
		fmt.Printf("PASSED>>: cluster configuration looks ok!\n")
		return
	}

	fmt.Printf("FAILED>>: cluster configuration does NOT look ok!\n")

	if !verbose {
		return
	}

	for idx, item := range items {
		fmt.Printf("ERROR#%03d: %s\n", idx, item.String())
	}
}

func NewClientWithScheme(scheme *k8sruntime.Scheme) (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}
