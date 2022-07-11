package validation

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNUMAResourcesOperatorReconciler(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Validation Suite")
}

var _ = ReportAfterSuite("Validation Suite", func(r Report) {
	fmt.Println()
})
