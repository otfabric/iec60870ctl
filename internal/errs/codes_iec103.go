// SPDX-License-Identifier: MIT

package errs

// Codes of iec103ctl (IEC 60870-5-103) that the other tools do not use.
const (
	// CodeIEC103LinkDown reports a request while the link to the protection equipment is
	// not initialized: it stopped answering the polls.
	CodeIEC103LinkDown = "link_down"
	// CodeIEC103TransferAborted reports a disturbance data transfer the protection
	// equipment aborted (IEC 60870-5-103 clause 7.4.7, TOO 34, 37 or 40).
	CodeIEC103TransferAborted = "transfer_aborted"
	// CodeIEC103NoIdentification reports equipment that sent no identification (ASDU 5)
	// after the reset of its link, where a command needs it.
	CodeIEC103NoIdentification = "no_identification"
)
