//go:build schedulerfixture && linux

package scheduler

// NewFixtureWorker is only available to the separately built isolated fixture.
// SQL, Target Permit, independent Supervisor and physical proof are unchanged.
// This constructor is absent from the privateworker-only command's build.
func NewFixtureWorker(store Store) *Worker { return &Worker{store: store, admission: true} }
