package dpm_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDpm(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DPM Suite")
}
