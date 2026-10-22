//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && terminationfixture

package integration

import "testing"

func TestBudgetPhysicalStopPG(t *testing.T) { exerciseNetworkPolicy(t, true) }
