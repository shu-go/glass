package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"

	"bitbucket.org/shu/elapsed"
	"bitbucket.org/shu/gli"
	"bitbucket.org/shu/rog"
)

type watchCmd struct {
	Target   string       `cli:"target, t" help:"target title"`
	Alpha    int          `cli:"alpha, a" help:"alpha by % (0 for unseen)"`
	Curve    float64      `cli:"curve, c" help:"alpha curve (power)"`
	Interval gli.Duration `cli:"interval, i" help:"watch interval"`
	Timeout  gli.Duration `help:"timeout of automatic recover"`
}

func (c *watchCmd) Init() {
	c.Alpha = defaultAlphaPercent
	c.Curve = defaultAlphaCurve
	c.Interval = gli.Duration(250 * time.Millisecond)
	c.Timeout = gli.Duration(0)
}

func (c *watchCmd) Before() error {
	if c.Alpha < 0 || 100 < c.Alpha {
		return fmt.Errorf("--alpha should be between 0 and 100")
	}

	if c.Curve < 1.0 || 3.0 < c.Curve {
		return fmt.Errorf("--curve should be between 1.0 and 3.0")
	}

	return nil
}

func (c *watchCmd) Run(args []string) error {
	rog.Debug("watch")
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

	fmt.Println("Press Ctrl+C to cancel.")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, os.Interrupt)

	var wins []*Window //save for listAllWindows() and recoverAlpha()

	var clswins []*Window

	var lastFG, currFG uintptr
	var lastRect, currRect Rect

	if c.Timeout != 0 {
		go func() {
			time.Sleep(c.Timeout.Duration())
			signalChan <- os.Interrupt
		}()
	}

wachLoop:
	for {
		select {
		case <-time.After(c.Interval.Duration()):
			//continue
		case <-signalChan:
			break wachLoop
		}

		currFG, _, _ = getForegroundWindow.Call()
		if result, _, _ := getWindowRect.Call(uintptr(currFG), uintptr(unsafe.Pointer(&currRect))); result == 0 {
			currRect = Rect{}
		}
		if (currFG == lastFG && currFG != 0) && (currRect == lastRect) {
			continue
		}
		lastFG = currFG
		lastRect = currRect

		verbose.Print("========== start ==========")
		tm := elapsed.Start()

		var err error
		wins, err = listAllWindows(true, wins)
		if err != nil {
			return err
		}
		verbose.Print("listed windows", tm.Elapsed())

		tgtwins := filterWindowsByTitle(wins, target)
		clswins = clswins[:0]
		clswins = append(clswins, wins...)

		verbose.Print("target filtered", tm.Elapsed())
		for _, w := range tgtwins {
			verbose.Printf("* %s", w.Title)
		}

		//

		z := makeZOrderedList(tgtwins, wins)
		level := len(z) - 1

		for depth, alpwins := range z {
			if depth == 0 {
				continue
			}

			verbose.Printf("---- %d (alpha=%d) ----\n", depth, alphaFromPercent(c.Alpha, level, c.Curve))
			for _, w := range alpwins {
				verbose.Printf("  %s (%d)\n", w.Title, w.PID)
				if uintptr(w.Handle) == currFG {
					setAnimatedAlpha(w.Handle, alphaFromPercent(c.Alpha, level, c.Curve), 200*time.Millisecond, 50*time.Millisecond)
				} else {
					setAlpha(w.Handle, alphaFromPercent(c.Alpha, level, c.Curve))
				}

				idx := -1
				for i, s := range clswins {
					if s.Handle == w.Handle {
						idx = i
					}
				}
				if idx != -1 {
					clswins = append(clswins[:idx], clswins[idx+1:]...)
				}
			}
			level--
		}

		for _, w := range clswins {
			setAlpha(w.Handle, 255)
		}
		verbose.Print("cleared", tm.Elapsed())
	}

	wins, _ = listAllWindows(true, wins)
	recoverAlpha(wins)

	return nil
}
