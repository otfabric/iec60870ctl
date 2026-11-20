// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestFlagAliases(t *testing.T) {
	var got uint16
	root := &cobra.Command{Use: "tool"}
	sub := &cobra.Command{Use: "sub", RunE: func(*cobra.Command, []string) error { return nil }}
	sub.Flags().Uint16Var(&got, "common-address", 1, "")
	root.AddCommand(sub)
	FlagAliases(root, map[string]string{"ca": "common-address"})
	for _, args := range [][]string{{"sub", "--ca", "7"}, {"sub", "--ca=8"}, {"sub", "--common-address", "9"}} {
		got = 0
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if want := map[string]uint16{"--ca": 7, "--ca=8": 8, "--common-address": 9}[args[1]]; got != want {
			t.Errorf("%v: %d, want %d", args, got, want)
		}
		if !sub.Flags().Changed("common-address") {
			t.Errorf("%v: common-address not marked as changed", args)
		}
	}
	if f := sub.Flags().Lookup("ca"); f == nil || f.Name != "common-address" {
		t.Errorf("lookup of the alias: %v", f)
	}
}
