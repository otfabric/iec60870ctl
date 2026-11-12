// SPDX-License-Identifier: MIT

package errs

import "testing"

// The exit codes of the organisation table (ADR-0006 of otfabric/engineering).
func TestOrganisationExitCodes(t *testing.T) {
	if got := ExitCodeForKind(KindInterrupted); got != 130 {
		t.Errorf("interrupted: exit %d, want 130", got)
	}
	if got := ExitCodeForKind(KindInternal); got != 10 {
		t.Errorf("internal: exit %d, want 10", got)
	}
	if ExitPartial != 7 {
		t.Errorf("partial: exit %d, want 7", ExitPartial)
	}
}
