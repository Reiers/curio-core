// Package curiowire is an integration scratch space: imports Curio
// packages to surface compilation gaps + transitive deps as we wire
// them into the curio-core bundle. Files here are pre-alpha
// experiments; they may be deleted or moved as the integration
// stabilizes.

package curiowire

import (
	// Smoke import — does tasks/pdp build at all in our environment?
	_ "github.com/filecoin-project/curio/tasks/pdp"
)
