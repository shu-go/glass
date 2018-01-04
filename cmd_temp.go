package main

import "fmt"

/*
	cli.StringFlag{Name: "target, t", Usage: "target title"},
	cli.IntFlag{Name: "alpha, a", Value: 50, Usage: "alpha by % (0 for unseen)"},
	cli.Float64Flag{Name: "curve, c", Value: defaultAlphaCurve, Usage: "alpha curve (power)"},
	cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
*/

type tempCmd struct {
	Target string  `cli:"target, t" help:"target title"`
	Alpha  int     `cli:"alpha, a" help:"alpha by % (0 for unseen)"`
	Curve  float64 `cli:"curve, c" help:"alpha curve (power)"`
}

func (c *tempCmd) Init() {
	c.Alpha = defaultAlphaPercent
	c.Curve = defaultAlphaCurve
}

func (c *tempCmd) Before() error {
	if c.Alpha < 0 || 100 < c.Alpha {
		return fmt.Errorf("--alpha should be between 0 and 100")
	}

	if c.Curve < 1.0 || 3.0 < c.Curve {
		return fmt.Errorf("--curve should be between 1.0 and 3.0")
	}

	return nil
}

func (c tempCmd) Run(args []string) error {
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
	level := len(z) - 1

	for depth, alpwins := range z {
		if depth == 0 {
			continue
		}

		verbose.Printf("---- %d ----\n", depth)
		for _, w := range alpwins {
			verbose.Printf("  %s\n", w.Title)
			setAlpha(w.Handle, alphaFromPercent(c.Alpha, level, c.Curve))
		}
		level--
	}
	return nil
}
