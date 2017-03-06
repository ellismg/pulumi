// Copyright 2016 Pulumi, Inc. All rights reserved.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pulumi/coconut/pkg/resource"
	"github.com/pulumi/coconut/pkg/tokens"
)

func newEnvConfigCmd() *cobra.Command {
	var unset bool
	cmd := &cobra.Command{
		Use:   "config <env> [<key> [value]]",
		Short: "Query, set, replace, or unset configuration values",
		Run: func(cmd *cobra.Command, args []string) {
			info := initEnvCmd(cmd, args)
			config := info.Env.Config
			if len(info.Args) == 0 {
				// If no args were supplied, we are just printing out the current configuration.
				if config != nil {
					fmt.Printf("%-32s %-32s\n", "KEY", "VALUE")
					for _, key := range resource.StableConfigKeys(info.Env.Config) {
						v := info.Env.Config[key]
						// TODO: print complex values.
						fmt.Printf("%-32s %-32s\n", key, v)
					}
				}
			} else {
				key := tokens.Token(info.Args[0])
				if config == nil {
					config = make(resource.ConfigMap)
					info.Env.Config = config
				}
				if len(info.Args) > 1 {
					// If there is a value, we are setting the configuration entry.
					// TODO: support values other than strings.
					config[key] = info.Args[1]
					saveEnv(info.Env, info.Old, "", true)
				} else {
					// If there was no value supplied, we are either reading or unsetting the entry.
					if unset {
						delete(config, key)
						saveEnv(info.Env, info.Old, "", true)
					} else if v, has := config[key]; has {
						// TODO: print complex values.
						fmt.Printf("%v\n", v)
					} else {
						exitError("configuration key '%v' not found for environment '%v'", key, info.Env.Name)
					}
				}
			}
		},
	}
	cmd.PersistentFlags().BoolVar(&unset, "unset", false, "Unset a configuration value")
	return cmd
}
