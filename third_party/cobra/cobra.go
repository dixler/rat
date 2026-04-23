package cobra

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

type PositionalArgs func(cmd *Command, args []string) error

type Command struct {
	Use   string
	Short string
	Args  PositionalArgs
	RunE  func(cmd *Command, args []string) error

	flagSet  *flag.FlagSet
	children []*Command
}

type FlagSet struct{ fs *flag.FlagSet }

func (c *Command) Flags() *FlagSet {
	if c.flagSet == nil {
		c.flagSet = flag.NewFlagSet(c.Use, flag.ContinueOnError)
		c.flagSet.SetOutput(os.Stderr)
	}
	return &FlagSet{fs: c.flagSet}
}

func (f *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	f.fs.BoolVar(p, name, value, usage)
}

func (c *Command) AddCommand(cmds ...*Command) {
	c.children = append(c.children, cmds...)
}

func (c *Command) Execute() error {
	return c.execute(os.Args[1:])
}

func (c *Command) execute(args []string) error {
	if len(args) > 0 {
		for _, child := range c.children {
			name := child.Use
			if i := indexSpace(name); i >= 0 {
				name = name[:i]
			}
			if args[0] == name {
				return child.execute(args[1:])
			}
		}
	}
	if c.flagSet != nil {
		if err := c.flagSet.Parse(args); err != nil {
			return err
		}
		args = c.flagSet.Args()
	}
	if c.Args != nil {
		if err := c.Args(c, args); err != nil {
			return fmt.Errorf("%s: %w", c.Use, err)
		}
	}
	if c.RunE == nil {
		return errors.New("no run configured")
	}
	return c.RunE(c, args)
}

func RangeArgs(min, max int) PositionalArgs {
	return func(_ *Command, args []string) error {
		if len(args) < min || len(args) > max {
			return fmt.Errorf("accepts between %d and %d arg(s), received %d", min, max, len(args))
		}
		return nil
	}
}

func indexSpace(s string) int {
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			return i
		}
	}
	return -1
}
