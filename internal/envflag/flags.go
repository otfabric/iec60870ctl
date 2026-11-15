// SPDX-License-Identifier: MIT

package envflag

import (
	"fmt"
	"reflect"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// RegisterFlags binds cobra flags to tagged fields on cfg (pointer to struct).
// Numeric and bool fields use reflect.Value.Addr().Interface().(*T) with the matching
// pflag *Var function — no unsafe.Pointer casts. Supported kinds: string, bool, []string,
// uint8–uint64, int, int8–int64, and uint.
func RegisterFlags(cmd *cobra.Command, cfg any) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		flagName := field.Tag.Get("flag")
		desc := field.Tag.Get("desc")
		envVar := field.Tag.Get("env")

		if field.Anonymous {
			RegisterFlags(cmd, v.Field(i).Addr().Interface())
			continue
		}

		if flagName == "" {
			continue
		}

		if envVar != "" {
			desc = fmt.Sprintf("%s [env: %s]", desc, envVar)
		}

		fv := v.Field(i)
		switch field.Type.Kind() {
		case reflect.String:
			cmd.Flags().StringVar(fv.Addr().Interface().(*string), flagName, fv.String(), desc)
		case reflect.Uint8:
			p := fv.Addr().Interface().(*uint8)
			cmd.Flags().Uint8Var(p, flagName, *p, desc)
		case reflect.Uint16:
			p := fv.Addr().Interface().(*uint16)
			cmd.Flags().Uint16Var(p, flagName, *p, desc)
		case reflect.Uint32:
			p := fv.Addr().Interface().(*uint32)
			cmd.Flags().Uint32Var(p, flagName, *p, desc)
		case reflect.Uint64:
			cmd.Flags().Uint64Var(fv.Addr().Interface().(*uint64), flagName, fv.Uint(), desc)
		case reflect.Uint:
			p := fv.Addr().Interface().(*uint)
			cmd.Flags().UintVar(p, flagName, *p, desc)
		case reflect.Int:
			p := fv.Addr().Interface().(*int)
			cmd.Flags().IntVar(p, flagName, *p, desc)
		case reflect.Int8:
			p := fv.Addr().Interface().(*int8)
			cmd.Flags().Int8Var(p, flagName, *p, desc)
		case reflect.Int16:
			p := fv.Addr().Interface().(*int16)
			cmd.Flags().Int16Var(p, flagName, *p, desc)
		case reflect.Int32:
			p := fv.Addr().Interface().(*int32)
			cmd.Flags().Int32Var(p, flagName, *p, desc)
		case reflect.Int64:
			cmd.Flags().Int64Var(fv.Addr().Interface().(*int64), flagName, fv.Int(), desc)
		case reflect.Bool:
			cmd.Flags().BoolVar(fv.Addr().Interface().(*bool), flagName, fv.Bool(), desc)
		case reflect.Slice:
			if field.Type.Elem().Kind() != reflect.String {
				panic(fmt.Sprintf("config.RegisterFlags: field %s: flag %q only supports []string slices", field.Name, flagName))
			}
			var cur []string
			if !fv.IsNil() {
				cur = fv.Interface().([]string)
			}
			cmd.Flags().StringSliceVar(fv.Addr().Interface().(*[]string), flagName, cur, desc)
		default:
			panic(fmt.Sprintf("config.RegisterFlags: field %s (%s) has flag %q but unsupported reflect.Kind %s — add a case or remove the flag tag",
				field.Name, field.Type.String(), flagName, field.Type.Kind()))
		}
		if envVar != "" {
			// Recorded for [FromEnvironment]; SetAnnotation fails only for a flag that does not exist.
			_ = cmd.Flags().SetAnnotation(flagName, envAnnotation, []string{envVar})
		}
	}
}

// envAnnotation is the annotation of a flag that names its environment variable.
const envAnnotation = "otfabric_env"

// FromEnvironment reports the environment variable that gave the value of the flag name of
// cmd: the variable bound to the flag by [RegisterFlags] when it is set and the flag is not
// on the command line. ok is false otherwise.
func FromEnvironment(cmd *cobra.Command, name string) (env string, ok bool) {
	f := cmd.Flags().Lookup(name)
	if f == nil || f.Changed {
		return "", false
	}
	vars := f.Annotations[envAnnotation]
	if len(vars) == 0 || Getenv(vars[0]) == "" {
		return "", false
	}
	return vars[0], true
}

// defaultAnnotation is the annotation of a flag that holds its built-in default: the value
// it has without its environment variable.
const defaultAnnotation = "otfabric_default"

// LoadAndRegister is [MustLoadFromEnv] and then [RegisterFlags] for cfg (a pointer to a
// struct), and records the built-in default of every flag, the value it has without its
// environment variable, for [CommandLineOnly].
func LoadAndRegister(cmd *cobra.Command, cfg any) {
	v := reflect.ValueOf(cfg).Elem()
	builtIn := reflect.New(v.Type())
	builtIn.Elem().Set(v)
	defaults := &cobra.Command{}
	RegisterFlags(defaults, builtIn.Interface())
	MustLoadFromEnv(cfg)
	RegisterFlags(cmd, cfg)
	defaults.Flags().VisitAll(func(f *pflag.Flag) {
		_ = cmd.Flags().SetAnnotation(f.Name, defaultAnnotation, []string{f.DefValue})
	})
}

// CommandLineOnly makes each of flags take its value from the command line alone. A flag
// whose value came from its environment variable gets its built-in default back, and one
// line on the standard error of cmd names the variable and the flag:
//
//	notice: IEC104CTL_FREEZE=reset is not used: --freeze is taken only from the command line
//
// Commands call it, before they parse or send anything, for the parameters that decide what
// a request changes at a station. The flags must have been registered by [LoadAndRegister].
func CommandLineOnly(cmd *cobra.Command, flags ...string) {
	for _, name := range flags {
		env, ok := FromEnvironment(cmd, name)
		if !ok {
			continue
		}
		f := cmd.Flags().Lookup(name)
		def, ok := f.Annotations[defaultAnnotation]
		if !ok || len(def) != 1 {
			panic(fmt.Sprintf("envflag.CommandLineOnly: --%s of %q has no built-in default (not registered by LoadAndRegister)", name, cmd.CommandPath()))
		}
		if err := f.Value.Set(def[0]); err != nil {
			panic(fmt.Sprintf("envflag.CommandLineOnly: --%s of %q: the built-in default %q: %v", name, cmd.CommandPath(), def[0], err))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "notice: %s=%s is not used: --%s is taken only from the command line\n", env, Getenv(env), name)
	}
}

// NotFromEnvironment returns an error that names the variable and the flag for the first of
// flags whose value came from its environment variable rather than from the command line,
// and nil when none did. It is for a parameter that is taken only from the command line and
// whose built-in default would do more at a station than the value of the variable asked
// for (a procedure that executes where the variable asked to select or to abort): the
// command refuses to run rather than go on without it.
func NotFromEnvironment(cmd *cobra.Command, flags ...string) error {
	for _, name := range flags {
		if env, ok := FromEnvironment(cmd, name); ok {
			return fmt.Errorf("%s=%s is not used: --%s is taken only from the command line; give --%s %s to run it so", env, Getenv(env), name, name, Getenv(env))
		}
	}
	return nil
}
