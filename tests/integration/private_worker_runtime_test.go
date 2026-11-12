//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture && privateworkerfixture

package integration

import "testing"

func TestPrivateWorkerLifecyclePG(t *testing.T) { runPeriodicSchedulerPG(t, true) }
