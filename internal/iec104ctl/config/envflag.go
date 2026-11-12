// SPDX-License-Identifier: MIT

package config

import "github.com/otfabric/iec60870ctl/internal/envflag"

// The environment and flag binding is shared by every tool of the repository
// (internal/envflag); these names keep the iec104ctl code as it was.
var (
	LoadFromEnv     = envflag.LoadFromEnv
	MustLoadFromEnv = envflag.MustLoadFromEnv
	RegisterFlags   = envflag.RegisterFlags
	Getenv          = envflag.Getenv
)

func init() {
	// IEC104CTL_OUTPUT_FORMAT is the older spelling of IEC104CTL_FORMAT (ADR-0006 of
	// otfabric/engineering); iec104ctl has accepted it since before the move.
	envflag.Legacy["IEC104CTL_FORMAT"] = "IEC104CTL_OUTPUT_FORMAT"
}
