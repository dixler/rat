package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"notectl/internal/getrefs"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var details bool
	root := &cobra.Command{
		Use:   "getrefs <file.go> [identifierName]",
		Short: "Color-cat Go files or show reference details",
		Args: func(_ *cobra.Command, args []string) error {
			if details {
				if len(args) != 1 {
					return errors.New("--details requires exactly one argument: [<file|dir>:]<identifierName>")
				}
				return nil
			}
			if len(args) < 1 || len(args) > 2 {
				return errors.New("requires 1 or 2 arguments")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if details {
				return getrefs.Run(args[0])
			}
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			return getrefs.Cat(args[0], name)
		},
	}
	root.Flags().BoolVar(&details, "details", false, "print reference/definition breakdown")
	root.AddCommand(newCatCmd())
	return root
}

func newCatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cat <file.go> [identifierName]",
		Short: "Compatibility alias for color-cat output",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			return getrefs.Cat(args[0], name)
		},
	}
}
