package main

import (
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"bitbucket.org/shu/elapsed"
	"bitbucket.org/shu/retry"

	//"golang.org/x/sys/windows"
	//"github.com/golang/sys/windows"

	"bitbucket.org/shu/log"
	"github.com/urfave/cli"
)

const (
	defaultAlphaPercent = 15
	defaultAlphaCurve   = 2
)

var (
	verbose = log.New(ioutil.Discard)
)

func initVerboseLog(c *cli.Context) {
	if c.GlobalBool("verbose") {
		verbose = log.New(os.Stderr)
	}
	verbose.SetFlags(log.NilHeader)
}

func main() {
	app := cli.NewApp()
	app.Name = "glass"
	app.Usage = "make overwrapping windows be transparent"
	app.Version = "0.4.0"
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "verbose", Usage: "verbose output to stderr"},
	}
	app.Commands = []cli.Command{
		{
			Name:  "watch",
			Usage: "run constantly",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "target, t", Usage: "target title"},
				cli.IntFlag{Name: "alpha, a", Value: defaultAlphaPercent, Usage: "alpha by % (0 for unseen)"},
				cli.Float64Flag{Name: "curve, c", Value: defaultAlphaCurve, Usage: "alpha curve (power)"},
				cli.DurationFlag{Name: "interval, i", Value: 250 * time.Millisecond, Usage: "watch interval"},
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				initVerboseLog(c)

				allprocs := c.Bool("allprocs")
				alpha := c.Int("alpha")
				if alpha < 0 || 100 < alpha {
					alpha = defaultAlphaPercent
				}
				curve := c.Float64("curve")
				if curve < 1.0 || 3.0 < curve {
					curve = defaultAlphaCurve
				}
				interval := c.Duration("interval")
				target := c.String("target")
				for _, v := range c.Args() {
					if len(target) > 0 {
						target += "\n"
					}
					target += v
				}
				if len(target) == 0 {
					return fmt.Errorf("target missing")
				}

				return runWatch(target, interval, alpha, curve, allprocs)
			},
		},
		{
			Name:    "list",
			Aliases: []string{"ls"},
			Usage:   "list overwrapping windows",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "target, t", Usage: "target title"},
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				initVerboseLog(c)

				allprocs := c.Bool("allprocs")
				target := c.String("target")
				for _, v := range c.Args() {
					if len(target) > 0 {
						target += "\n"
					}
					target += v
				}
				if len(target) == 0 {
					return fmt.Errorf("target missing")
				}

				return runList(target, allprocs)
			},
		},
		{
			Name:  "temp",
			Usage: "run once",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "target, t", Usage: "target title"},
				cli.IntFlag{Name: "alpha, a", Value: 50, Usage: "alpha by % (0 for unseen)"},
				cli.Float64Flag{Name: "curve, c", Value: defaultAlphaCurve, Usage: "alpha curve (power)"},
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				initVerboseLog(c)

				allprocs := c.Bool("allprocs")
				alpha := c.Int("alpha")
				if alpha < 0 || 100 < alpha {
					alpha = 50
				}
				curve := c.Float64("curve")
				if curve < 1.0 || 3.0 < curve {
					curve = defaultAlphaCurve
				}
				target := c.String("target")
				for _, v := range c.Args() {
					if len(target) > 0 {
						target += "\n"
					}
					target += v
				}
				if len(target) == 0 {
					return fmt.Errorf("target missing")
				}

				return runTemp(target, alpha, curve, allprocs)
			},
		},
		{
			Name:  "recover",
			Usage: "force all windows untransparent",
			Flags: []cli.Flag{
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				initVerboseLog(c)

				allprocs := c.Bool("allprocs")

				return runRecover(allprocs)
			},
		},
	}
	app.Run(os.Args)
	return
}

func runList(target string, allprocs bool) error {
	wins, err := listAllWindows(allprocs, nil)
	if err != nil {
		return err
	}

	tgtwins := filterWindowsByTitle(wins, target)

	z := makeZOrderedList(tgtwins, wins)
	for depth, wins := range z {
		fmt.Printf("---- %d ----\n", depth)
		for _, w := range wins {
			fmt.Printf("  %s(%s)\n", w.Title, w.PID)
		}
	}

	return nil
}

func runTemp(target string, alpha int, curve float64, allprocs bool) error {
	wins, err := listAllWindows(allprocs, nil)
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
			setAlpha(w.Handle, alphaFromPercent(alpha, level, curve))
		}
		level--
	}
	return nil
}

func runRecover(allprocs bool) error {
	wins, err := listAllWindows(allprocs, nil)
	if err != nil {
		return err
	}

	for _, w := range wins {
		setAlpha(w.Handle, 255)
	}

	return nil
}

func runWatch(target string, interval time.Duration, alpha int, curve float64, allprocs bool) error {
	fmt.Println("Press Ctrl+C to cancel.")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, os.Interrupt)

	var wins []*Window //save for listAllWindows() and recoverAlpha()

	var clswins []*Window

	var lastFG, currFG uintptr
	var lastRect, currRect Rect

wachLoop:
	for {
		select {
		case <-time.After(interval):
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
		wins, err = listAllWindows(allprocs, wins)
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

			verbose.Printf("---- %d (alpha=%d) ----\n", depth, alphaFromPercent(alpha, level, curve))
			for _, w := range alpwins {
				verbose.Printf("  %s (%d)\n", w.Title, w.PID)
				if uintptr(w.Handle) == currFG {
					setAnimatedAlpha(w.Handle, alphaFromPercent(alpha, level, curve), 200*time.Millisecond, 50*time.Millisecond)
				} else {
					setAlpha(w.Handle, alphaFromPercent(alpha, level, curve))
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

	wins, _ = listAllWindows(allprocs, wins)
	recoverAlpha(wins)

	return nil
}

const (
	GWL_EXSTYLE      = 0xFFFFFFEC
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_LAYERED    = 0x80000

	LWA_COLORKEY = 0x1
	LWA_ALPHA    = 0x2
)

type (
	Rect struct {
		Left, Top, Right, Bottom int32
	}
	Window struct {
		Title       string
		Handle      syscall.Handle
		PID         int
		ZPrevHandle syscall.Handle
		Rect        Rect
		OrgAlpha    int
	}
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	isWindow                 = user32.NewProc("IsWindow")
	getForegroundWindow      = user32.NewProc("GetForegroundWindow")
	enumWindows              = user32.NewProc("EnumWindows")
	getWindowText            = user32.NewProc("GetWindowTextW")
	getWindowTextLength      = user32.NewProc("GetWindowTextLengthW")
	getWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")

	getWindow = user32.NewProc("GetWindow")

	getWindowRect = user32.NewProc("GetWindowRect")

	getLayeredWindowAttributes = user32.NewProc("GetLayeredWindowAttributes")
	setLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	getWindowLong              = user32.NewProc("GetWindowLongW")
	setWindowLong              = user32.NewProc("SetWindowLongW")
	isWindowVisible            = user32.NewProc("IsWindowVisible")
	isIconic                   = user32.NewProc("IsIconic")

	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	//-> use windows.XXX
)

func listAllWindows(allprocs bool, orgWins []*Window) (wins []*Window, err error) {
	orgDict := makeHWND2WindowDict(orgWins)

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lparam uintptr) uintptr {
		b, _, _ := isWindow.Call(uintptr(hwnd))
		if b == 0 {
			return 1
		}

		title := ""
		tlen, _, _ := getWindowTextLength.Call(uintptr(hwnd))
		if tlen != 0 {
			tlen++
			buff := make([]uint16, tlen)
			getWindowText.Call(
				uintptr(hwnd),
				uintptr(unsafe.Pointer(&buff[0])),
				uintptr(tlen),
			)
			title = syscall.UTF16ToString(buff)
		}

		prevHWND := syscall.Handle(uintptr(0))
		result, _, _ := getWindow.Call(uintptr(hwnd), 3 /*GW_HWNDPREV*/)
		if result != 0 {
			prevHWND = syscall.Handle(uintptr(result))
		}

		var processID uintptr
		getWindowThreadProcessId.Call(
			uintptr(hwnd),
			uintptr(unsafe.Pointer(&processID)),
		)

		r := Rect{}
		result, _, _ = getWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
		if result == 0 {
			r = Rect{}
		}

		var orgWin *Window
		if orgDict != nil {
			if w, found := orgDict[hwnd]; found {
				orgWin = w
			}
		}

		var alpha uintptr
		if orgWin != nil {
			alpha = uintptr(orgWin.OrgAlpha)
		} else {
			var flag uintptr
			result, _, _ := getLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flag)))
			if result == 0 || flag&LWA_ALPHA == 0 {
				alpha = 255
			}
			/*
				style, _, err := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
				if style&WS_EX_LAYERED != 0 {
					result, _, _ := getLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(&alpha)), LWA_ALPHA)
					if result == 0 {
						alpha = 255
					}
				} else {
					alpha = 255
				}
			*/
		}

		win := &Window{
			Title:       title,
			Handle:      hwnd,
			PID:         int(processID),
			ZPrevHandle: prevHWND,
			Rect:        r,
			OrgAlpha:    int(alpha),
		}
		wins = append(wins, win)

		return 1
	})

	a, _, _ := enumWindows.Call(cb, 0)
	if a == 0 {
		return nil, fmt.Errorf("USER32.EnumWindows returned FALSE")
	}

	return wins, nil
}

func filterWindowsByTitle(wins []*Window, filter string) []*Window {
	var filters []string
	{
		ff := strings.Split(filter, "\n")
		filters = make([]string, 0, len(ff))
		for _, f := range ff {
			filters = append(filters, strings.ToUpper(f))
		}
	}

	pid := syscall.Getpid()
	ppid := syscall.Getppid()

	var results []*Window
	for _, w := range wins {
		if w.PID == pid || w.PID == ppid {
			continue
		}

		if visible, _, _ := isWindowVisible.Call(uintptr(w.Handle)); visible == 0 {
			continue
		}

		if iconic, _, _ := isIconic.Call(uintptr(w.Handle)); iconic != 0 {
			continue
		}

		ok := false
		for _, f := range filters {
			if strings.Contains(strings.ToUpper(w.Title), f) {
				ok = true
			}
		}

		if ok {
			results = append(results, w)
		}
	}

	return results
}

func (w *Window) dump() string {
	if w == nil {
		return ""
	}
	return fmt.Sprintf("%q(HWND=%v, PID=%v) %#v", w.Title, w.Handle, w.PID, w.Rect)
}

func makeZOrderedList(tgts []*Window, all []*Window) [][]*Window {
	dict := makeHWND2WindowDict(all)

	// z ... background-to-foreground-ordered index
	// [0] ... most background
	// [1] ... one step foreground(prev)
	z := make([][]*Window, 1)
	z[0] = tgts[:]

	for _, z0 := range z[0] {
		curr := z0
		i := 0
		for {
			// break inf loop
			i++
			if i > 200 {
				verbose.Print(i, curr, curr.ZPrevHandle)
				if i > 250 {
					break
				}
			}

			if prev, found := dict[curr.ZPrevHandle]; found {
				if !isWindowOverwrapping(prev, z0) {
					curr = prev
					continue
				}

				depth := 1
			depth:
				for d, wins := range z {
					if d == 0 {
						continue
					}

					for _, w := range wins {
						if prev.PID == w.PID {
							curr = prev
							continue
						}

						tiled := true
						if isWindowOverwrapping(prev, w) {
							depth = d + 1
							tiled = false
						}
						if tiled {
							break depth
						}
					}
				}

				if len(z) < depth+1 {
					z = append(z, make([]*Window, 0))
				}
				z[depth] = append(z[depth], prev)

				curr = prev
			} else {
				break
			}

		}
	}

	return z
}

func isWindowOverwrapping(w1, w2 *Window) bool {
	w2.Rect.Left += (w2.Rect.Right - w2.Rect.Left) / 10
	w2.Rect.Top += (w2.Rect.Bottom - w2.Rect.Top) / 10
	w2.Rect.Right -= (w2.Rect.Right - w2.Rect.Left) / 10
	w2.Rect.Bottom -= (w2.Rect.Bottom - w2.Rect.Top) / 10

	overwrapping := w1.Rect.Left <= w2.Rect.Right && w2.Rect.Left <= w1.Rect.Right &&
		w1.Rect.Top <= w2.Rect.Bottom && w2.Rect.Top <= w1.Rect.Bottom

	visible1, _, _ := isWindowVisible.Call(uintptr(w1.Handle))
	visible2, _, _ := isWindowVisible.Call(uintptr(w2.Handle))
	iconic1, _, _ := isIconic.Call(uintptr(w1.Handle))
	iconic2, _, _ := isIconic.Call(uintptr(w2.Handle))

	style1, _, _ := getWindowLong.Call(uintptr(w2.Handle), GWL_EXSTYLE)

	return overwrapping &&
		visible1 != 0 && iconic1 == 0 &&
		visible2 != 0 && iconic2 == 0 &&
		style1&WS_EX_TOOLWINDOW == 0
}

func makeHWND2WindowDict(wins []*Window) map[syscall.Handle]*Window {
	var d map[syscall.Handle]*Window
	if len(wins) != 0 {
		d = make(map[syscall.Handle]*Window)
		for _, w := range wins {
			d[w.Handle] = w
		}
		return d
	}
	return nil
}

func setAlpha(hwnd syscall.Handle, alpha uintptr) {
	if alpha == 0 {
		alpha = 1
	}

	if alpha == 255 {
		var currAlpha uintptr = 255
		{
			style, _, _ := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
			if style&WS_EX_LAYERED != 0 {
				var flag uintptr
				result, _, _ := getLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(&currAlpha)), uintptr(unsafe.Pointer(&flag)))
				if result == 0 || flag&LWA_ALPHA == 0 {
					currAlpha = 255
				}
			}
		}
		if currAlpha != 255 {
			setLayeredWindowAttributes.Call(uintptr(hwnd), 0, 255, LWA_ALPHA)
			style, _, _ := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
			// clear WS_EX_LAYERED bit
			setWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE, style&^WS_EX_LAYERED)
		}
	} else {
		style, _, _ := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
		setWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE, style|WS_EX_LAYERED)
		setLayeredWindowAttributes.Call(uintptr(hwnd), 0, alpha, LWA_ALPHA)
	}
}

func setAnimatedAlpha(hwnd syscall.Handle, alpha uintptr, timeout, wait time.Duration) {
	if alpha == 0 {
		alpha = 1
	}

	if alpha == 255 {
		setAlpha(hwnd, alpha)
	} else {
		style, _, _ := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
		setWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE, style|WS_EX_LAYERED)

		var currAlpha uintptr = 255
		{
			var flag uintptr
			result, _, _ := getLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(&currAlpha)), uintptr(unsafe.Pointer(&flag)))
			if result == 0 || flag&LWA_ALPHA == 0 {
				currAlpha = 255
			}
		}
		if currAlpha != alpha {
			var ca uintptr = 255
			times := uintptr(math.Max(1, float64(int(timeout/wait))))
			retry.Wait(timeout, wait, func() bool {
				setLayeredWindowAttributes.Call(uintptr(hwnd), 0, ca, LWA_ALPHA)
				ca -= (255 - alpha) / times
				return false
			})
		}

		if currAlpha != alpha {
			setLayeredWindowAttributes.Call(uintptr(hwnd), 0, alpha, LWA_ALPHA)
		}
	}
}

func recoverAlpha(wins []*Window) {
	for _, w := range wins {
		style, _, _ := getWindowLong.Call(uintptr(w.Handle), GWL_EXSTYLE)
		if style&WS_EX_LAYERED == 0 {
			continue
		}

		if w.OrgAlpha == 255 {
			//setWindowLong.Call(uintptr(w.Handle), GWL_EXSTYLE, style&^WS_EX_LAYERED)
			setAlpha(w.Handle, 255)
		} else {
			/*
				_, _, err := setLayeredWindowAttributes.Call(uintptr(w.Handle), 0, uintptr(255*float64(100-w.OrgAlpha)/100), LWA_ALPHA)
				if err != nil {
					log.Printf("%q SetLayeredWindowAttributeserr=%v", w.Title, err)
				}
			*/
			setAlpha(w.Handle, uintptr(w.OrgAlpha))
		}
	}
}

func alphaFromPercent(percent, level int, curve float64) uintptr {
	return uintptr(255 * math.Pow(float64(100-percent)/100, math.Pow(float64(level), curve)))
}
