// SPDX-License-Identifier: MIT

package iec

import "github.com/otfabric/go-iec60870-5/iec104/client"

// SetMonitorReconnect sets the backoff of the monitor's reconnection for a test of package
// iec_test, and returns a function that restores it.
func SetMonitorReconnect(r client.Reconnect) (restore func()) {
	old := monitorReconnect
	monitorReconnect = r
	return func() { monitorReconnect = old }
}
