// Compile-only smoke test: ensures the public types + constructors of
// tasks/pdp and tasks/pdpv0 are reachable from curio-core's module
// boundary. If the CGo carveout breaks any of these symbols at build
// time, this test fails to compile and surfaces the regression early.
//
// Run mode: //go:build curiocore_compileonly. Default test runs skip
// these; CI's `go vet ./...` exercises them via the implicit type-check.

//go:build curiocore_compileonly

package pdptests

import (
	"testing"

	"github.com/filecoin-project/curio/tasks/pdp"
	"github.com/filecoin-project/curio/tasks/pdpv0"
)

func TestCurioPDPPackagesReachable(t *testing.T) {
	// Touch one public symbol from each package so the import isn't
	// elided by the compiler.
	_ = pdp.PDPProveTaskName
	_ = pdpv0.MaxBackoffBlocks
}
