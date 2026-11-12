// SPDX-License-Identifier: MIT

package errs

// Codes of iec102ctl: the causes of refusal of IEC 60870-5-102 (Beckhoff TS650x manual,
// section 9.1.11) and the REE session. They are reported with KindNegative (exit 6).
const (
	// The meter has no such record (list), cause 13.
	CodeNoRecord = "no_record"
	// The record number is unknown, cause 15.
	CodeUnknownRecordNumber = "unknown_record_number"
	// The record address (or the common address) is unknown, cause 16.
	CodeUnknownRecordAddr = "unknown_record_address"
	// The requested information object is not available, cause 17.
	CodeNoObject = "no_object"
	// The requested integration period is not available, cause 18.
	CodeNoPeriod = "no_period"
	// REE profile: the meter refused or did not confirm the access key (C_AC_NA_2).
	CodeLoginRefused = "login_refused"
)
