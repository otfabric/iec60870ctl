// SPDX-License-Identifier: MIT

package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CompleteRest finishes the shell completion of the command tree under root, after every
// command has registered the completion of its fixed value sets:
//
//   - a flag that takes a value and has no completion of its own completes file names when
//     files names it (only those with one of its extensions, if it lists any) and nothing
//     otherwise: addresses, numbers and other free values are not file names;
//   - a command that does not complete its arguments completes no file names.
//
// args names the commands, by their path without the root ("client script"), whose
// arguments are files, with the extensions to offer.
func CompleteRest(root *cobra.Command, files map[string][]string, args map[string][]string) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if t := f.Value.Type(); t == "bool" || t == "count" || HasCompletion(c, f) {
				return // takes no value, or completes already
			}
			if exts, ok := files[f.Name]; ok {
				_ = c.MarkFlagFilename(f.Name, exts...)
				return
			}
			_ = c.RegisterFlagCompletionFunc(f.Name, cobra.NoFileCompletions)
		})
		if c.ValidArgsFunction == nil && len(c.ValidArgs) == 0 {
			if exts, ok := args[pathOf(c)]; ok {
				c.ValidArgsFunction = fileArgs(exts)
			} else {
				c.ValidArgsFunction = cobra.NoFileCompletions
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// HasCompletion reports whether flag f of c completes: a completion function, or a
// file name or directory annotation.
func HasCompletion(c *cobra.Command, f *pflag.Flag) bool {
	if _, ok := c.GetFlagCompletionFunc(f.Name); ok {
		return true
	}
	_, files := f.Annotations[cobra.BashCompFilenameExt]
	_, dirs := f.Annotations[cobra.BashCompSubdirsInDir]
	return files || dirs
}

func pathOf(c *cobra.Command) string {
	if !c.HasParent() {
		return ""
	}
	if p := pathOf(c.Parent()); p != "" {
		return p + " " + c.Name()
	}
	return c.Name()
}

func fileArgs(exts []string) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(exts) == 0 {
			return nil, cobra.ShellCompDirectiveDefault
		}
		return exts, cobra.ShellCompDirectiveFilterFileExt
	}
}
