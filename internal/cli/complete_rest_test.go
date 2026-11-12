// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runComplete runs the hidden __complete command of root and returns the candidates and the
// directive line.
func runComplete(t *testing.T, root *cobra.Command, args ...string) ([]string, string) {
	t.Helper()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	return lines[:len(lines)-1], lines[len(lines)-1]
}

func TestCompleteRest(t *testing.T) {
	newTree := func() *cobra.Command {
		root := &cobra.Command{Use: "tool"}
		root.PersistentFlags().String("addr", "", "address")
		run := &cobra.Command{Use: "run", Run: func(*cobra.Command, []string) {}}
		run.Flags().String("config", "", "model file")
		run.Flags().String("mode", "", "a or b")
		run.Flags().Bool("yes", false, "")
		run.Flags().String("cert", "", "PEM file")
		_ = RegisterEnumFlagCompletion(run, "mode", []string{"a", "b"})
		_ = run.MarkFlagFilename("cert", "pem")
		script := &cobra.Command{Use: "script", Run: func(*cobra.Command, []string) {}}
		load := &cobra.Command{Use: "load", Run: func(*cobra.Command, []string) {}}
		anyFile := &cobra.Command{Use: "any", Run: func(*cobra.Command, []string) {}}
		root.AddCommand(run, script, load, anyFile)
		CompleteRest(root, map[string][]string{"config": {"yaml", "json"}},
			map[string][]string{"script": {"yaml"}, "any": nil})
		return root
	}
	nofile := ":4" // ShellCompDirectiveNoFileComp
	for _, c := range []struct {
		args      []string
		want      []string
		directive string
	}{
		{[]string{"run", "--addr", ""}, nil, nofile},
		{[]string{"run", "--mode", ""}, []string{"a", "b"}, nofile},
		{[]string{"run", "--config", ""}, []string{"yaml", "json"}, ":8"},
		{[]string{"run", "--cert", ""}, []string{"pem"}, ":8"},
		{[]string{"run", ""}, nil, nofile},
		{[]string{"script", ""}, []string{"yaml"}, ":8"},
		{[]string{"any", ""}, nil, ":0"},
		{[]string{"load", ""}, nil, nofile},
	} {
		got, directive := runComplete(t, newTree(), c.args...)
		if !slices.Equal(got, c.want) || directive != c.directive {
			t.Errorf("%v: %q %s; want %q %s", c.args, got, directive, c.want, c.directive)
		}
	}
}
