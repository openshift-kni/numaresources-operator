package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mdomke/git-semver/version"
)

func showVersion() int {
	if ver, ok := os.LookupEnv("NRO_BUILD_VERSION"); ok {
		fmt.Println(ver)
		return 0
	}

	v, err := version.Derive()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	fmt.Println(v)
	return 0
}

func showCommit() int {
	if cm, ok := os.LookupEnv("NRO_BUILD_COMMIT"); ok {
		fmt.Println(cm)
		return 0
	}

	cmd := exec.Command("git", "log", "-1", "--pretty=format:%h")
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	fmt.Println(strings.TrimSpace(string(out)))
	return 0
}

func help() {
	fmt.Fprintf(os.Stderr, "usage: %s [version|commit]\n", filepath.Base(os.Args[0]))
	os.Exit(1)
}

func main() {
	if len(os.Args) != 2 {
		help()
	}

	switch os.Args[1] {
	case "version":
		showVersion()
	case "commit":
		showCommit()
	default:
		help()
	}
}
