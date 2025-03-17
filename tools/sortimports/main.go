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

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

type Args struct {
	Paths       []string
	ExcludeDirs sets.Set[string]
	Verbose     bool
}

// Sorts imports based on:
// 1. Standard libraries
// 2. "k8s.io" imports
// 3. "sigs.k8s.io" imports
// 4. Other third-party libraries
// 5. "github.com/openshift" imports
// 6. "github.com/openshift-kni" imports
// Custom sorting function for import specs
func sortImports(imports []*ast.ImportSpec) []*ast.ImportSpec {
	var stdLibs, k8sLibs, thirdParty, openShiftLibs, openshiftKniLibs []*ast.ImportSpec

	for _, imp := range imports {
		path := strings.Trim(imp.Path.Value, `"`)
		switch {
		case isStdLib(path):
			stdLibs = append(stdLibs, imp)
		case strings.HasPrefix(path, "k8s.io") || strings.HasPrefix(path, "sigs.k8s.io"):
			k8sLibs = append(k8sLibs, imp)
		case strings.HasPrefix(path, "github.com/openshift-kni"):
			openshiftKniLibs = append(openshiftKniLibs, imp)
		case strings.HasPrefix(path, "github.com/openshift"):
			openShiftLibs = append(openShiftLibs, imp)
		default:
			thirdParty = append(thirdParty, imp)
		}
	}

	// Sort each group while keeping original comments and aliases
	sort.Slice(stdLibs, func(i, j int) bool { return stdLibs[i].Path.Value < stdLibs[j].Path.Value })
	sort.Slice(k8sLibs, func(i, j int) bool { return k8sLibs[i].Path.Value < k8sLibs[j].Path.Value })
	sort.Slice(thirdParty, func(i, j int) bool { return thirdParty[i].Path.Value < thirdParty[j].Path.Value })
	sort.Slice(openShiftLibs, func(i, j int) bool { return openShiftLibs[i].Path.Value < openShiftLibs[j].Path.Value })
	sort.Slice(openshiftKniLibs, func(i, j int) bool { return openshiftKniLibs[i].Path.Value < openshiftKniLibs[j].Path.Value })

	// Reconstruct sorted import block
	var sortedImports []*ast.ImportSpec
	sortedImports = append(sortedImports, stdLibs...)
	if len(k8sLibs) > 0 {
		sortedImports = append(sortedImports, k8sLibs...)
	}
	if len(thirdParty) > 0 {
		sortedImports = append(sortedImports, thirdParty...)
	}
	if len(openShiftLibs) > 0 {
		sortedImports = append(sortedImports, openShiftLibs...)
	}
	if len(openshiftKniLibs) > 0 {
		sortedImports = append(sortedImports, openshiftKniLibs...)
	}

	return sortedImports
}

// Determines if a package is a standard library package
func isStdLib(pkg string) bool {
	// Assume anything without a dot (.) is a std lib package
	return !strings.Contains(pkg, ".")
}

// Process the file while preserving alias & comments in imports
func processFile(src []byte) ([]byte, error) {
	// Parse the file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Extract imports
	var imports []*ast.ImportSpec
	var importDecl *ast.GenDecl

	// Find import declaration
	for _, decl := range node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecl = genDecl
			for _, spec := range genDecl.Specs {
				imports = append(imports, spec.(*ast.ImportSpec))
			}
		}
	}

	// do nothing if no imports are found
	if importDecl == nil {
		return src, nil
	}

	// Sort imports
	sortedImports := sortImports(imports)

	// Replace original imports with sorted ones
	importDecl.Specs = nil
	for _, imp := range sortedImports {
		importDecl.Specs = append(importDecl.Specs, imp)
	}

	// Format & write back the file
	var formattedCode bytes.Buffer
	err = format.Node(&formattedCode, fset, node)
	if err != nil {
		return nil, fmt.Errorf("failed to format code: %v", err)
	}
	return formattedCode.Bytes(), nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <path-to-files>\n", os.Args[0])
	}
	progArgs, err := parseFlags(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	for _, rootPath := range progArgs.Paths {
		err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				if progArgs.ExcludeDirs.Has(path) {
					return filepath.SkipDir
				}
				// we'll return to the files in this dir later
				return nil
			}
			// skip non Go files
			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			if progArgs.Verbose {
				fmt.Printf("sorting imports for file: %q\n", path)
			}

			src, err2 := os.ReadFile(path)
			if err2 != nil {
				return fmt.Errorf("failed to read file: %w", err2)
			}

			sortedSrc, err2 := processFile(src)
			if err2 != nil {
				return fmt.Errorf("failed to sort imports: %w", err2)
			}

			if err2 := os.WriteFile(path, sortedSrc, 0644); err2 != nil {
				return fmt.Errorf("failed to write file: %w", err2)
			}
			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	}
}

func parseFlags(args []string) (*Args, error) {
	progArgs := &Args{
		Paths:       make([]string, 0),
		ExcludeDirs: make(sets.Set[string]),
	}
	paths := flag.String("paths", "", "Path to files to be sorted, multiple paths can be provided.")
	excludeDirs := flag.String("exclude-dirs", "", "Directory names that should be excluded from imports, multiple directories can be provided.")
	flag.BoolVar(&progArgs.Verbose, "verbose", false, "Verbose output")
	flag.Parse()

	if err := prepPaths(*paths, progArgs); err != nil {
		return nil, err
	}

	if err := prepExcludeDirs(*excludeDirs, progArgs); err != nil {
		return nil, err
	}
	return progArgs, nil
}

func prepPaths(paths string, progArgs *Args) error {
	if paths == "" {
		return fmt.Errorf("no paths specified")
	}
	progArgs.Paths = strings.Split(paths, " ")
	return cleanPaths(progArgs.Paths)
}

func prepExcludeDirs(excludeDirs string, progArgs *Args) error {
	if excludeDirs == "" {
		return nil
	}
	dirs := strings.Split(excludeDirs, " ")
	if err := cleanPaths(dirs); err != nil {
		return err
	}
	for _, dir := range dirs {
		progArgs.ExcludeDirs.Insert(dir)
	}
	return nil
}

func cleanPaths(paths []string) error {
	var errs []error
	for i, path := range paths {
		cleanPath := filepath.Clean(path)
		if cleanPath == "" {
			errs = append(errs, fmt.Errorf("path %q is invalid", path))
			continue
		}
		paths[i] = cleanPath
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
