package main

import "fmt"

type listCmd struct {
	Target string `cli:"target, t" help:"target title"`
}

func (c listCmd) Run(args []string, global globalCmd) error {
	target := c.Target
	for _, v := range args {
		if len(target) > 0 {
			target += "\n"
		}
		target += v
	}
	if len(target) == 0 {
		return fmt.Errorf("target missing")
	}

	wins, err := listAllWindows(nil, global.EnableColorProfiling)
	if err != nil {
		return err
	}

	tgtwins := filterWindowsByTitle(wins, target)

	z := makeZOrderedList(tgtwins, wins)
	for depth, wins := range z {
		fmt.Printf("---- %d ----\n", depth)
		for _, w := range wins {
			fmt.Printf("  %s(%d) %d\n", w.Title, w.PID, w.ColorProfile.AvgGray(255))
		}
	}

	return nil

}
