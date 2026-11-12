// SPDX-License-Identifier: MIT

package config

import "github.com/otfabric/iec60870ctl/internal/envflag"

// The environment and flag binding is shared by every tool of the repository
// (internal/envflag).
var (
	LoadFromEnv     = envflag.LoadFromEnv
	MustLoadFromEnv = envflag.MustLoadFromEnv
	RegisterFlags   = envflag.RegisterFlags
	Getenv          = envflag.Getenv
)
