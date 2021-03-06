package main

import "fmt"

type listCmd struct {
	Target string `cli:"target, t" help:"target title"`
}

func (c listCmd) Run(args []string) error {
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

	wins, err := listAllWindows(true, nil)
	if err != nil {
		return err
	}

	tgtwins := filterWindowsByTitle(wins, target)

	z := makeZOrderedList(tgtwins, wins)
	for depth, wins := range z {
		fmt.Printf("---- %d ----\n", depth)
		for _, w := range wins {
			fmt.Printf("  %s(%d)\n", w.Title, w.PID)
		}
	}

	return nil

}
