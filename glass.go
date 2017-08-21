package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"

	//"golang.org/x/sys/windows"
	//"github.com/golang/sys/windows"

	"github.com/urfave/cli"
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

	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	//-> use windows.XXX
)

const (
	INFINITE     = 0xFFFFFFFF
	SYNCHRONIZE  = 0x00100000
	WAIT_TIMEOUT = 0x00000102

	TH32CS_SNAPPROCESS = 0x00000002

	WS_EX_LAYERED = 0x80000
	LWA_COLORKEY  = 0x1
	LWA_ALPHA     = 0x2
	GWL_EXSTYLE   = 0xFFFFFFEC
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
			if strings.Contains(title, "GVIM") {
				log.Printf("  %q org=%v, alpha=%v", title, orgWin.dump(), orgWin.OrgAlpha)
			}
			alpha = uintptr(orgWin.OrgAlpha)
		} else {
			var flag uintptr
			result, _, err := getLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flag)))
			if strings.Contains(title, "GVIM") {
				log.Printf("  %q alpha=%v result=%v err=%v", title, alpha, result, err)
			}
			if result == 0 || flag&LWA_ALPHA == 0 {
				alpha = 255
			}
			/*
				style, _, err := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
				//if strings.Contains(title, "GVIM") {
				log.Printf("  %q style=%v err=%v", title, style&WS_EX_LAYERED, err)
				//}
				if style&WS_EX_LAYERED != 0 {
					result, _, _ := getLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(&alpha)), LWA_ALPHA)
					if result == 0 {
						alpha = 255
					}
				} else {
					if strings.Contains(title, "GVIM") {
						log.Printf("  %q else", title)
					}
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

func main() {
	app := cli.NewApp()
	app.Name = "glass"
	app.Usage = "make overwrapping windows be transparent"
	app.Version = "0.1.0"
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "verbose", Usage: "verbose output to stderr"},
	}
	app.Commands = []cli.Command{
		{
			Name:  "watch",
			Usage: "run constantly",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "target, t", Usage: "target title"},
				cli.IntFlag{Name: "alpha, a", Value: 50, Usage: "alpha by % (0 for unseen)"},
				cli.DurationFlag{Name: "interval, i", Value: 1 * time.Second, Usage: "watch interval"},
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				allprocs := c.Bool("allprocs")
				alpha := c.Int("alpha")
				if alpha < 0 || 100 < alpha {
					alpha = 50
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

				return runWatch(target, interval, alpha, allprocs)
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
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				allprocs := c.Bool("allprocs")
				alpha := c.Int("alpha")
				if alpha < 0 || 100 < alpha {
					alpha = 50
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

				return runTemp(target, alpha, allprocs)
			},
		},
		{
			Name:  "recover",
			Usage: "force all windows untransparent",
			Flags: []cli.Flag{
				cli.BoolTFlag{Name: "allprocs, all", Usage: "include windows created by all users"},
			},
			Action: func(c *cli.Context) error {
				allprocs := c.Bool("allprocs")

				return runRecover(allprocs)
			},
		},
	}
	app.Run(os.Args)
	return
}

func filterWindowsByTitle(wins []*Window, filter string) []*Window {
	var filters []string
	{
		ff := strings.Split(filter, " ")
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

		if isvisible, _, _ := isWindowVisible.Call(uintptr(w.Handle)); isvisible == 0 {
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

func runList(target string, allprocs bool) error {
	wins, err := listAllWindows(allprocs, nil)
	if err != nil {
		return err
	}

	tgtwins := filterWindowsByTitle(wins, target)

	for _, w := range tgtwins {
		fmt.Printf("%s\n", w.Title)

		root := makeZOrderGraph(w, wins)
		filterGraphOverwrapping(root, w)
		curr := root
		for {
			curr = curr.Prev
			if curr == nil {
				break
			}

			fmt.Printf("  %s\n", curr.Window.Title)
		}
	}

	return nil
}

func runTemp(target string, alpha int, allprocs bool) error {
	wins, err := listAllWindows(allprocs, nil)
	if err != nil {
		return err
	}

	tgtwins := filterWindowsByTitle(wins, target)

	for _, w := range tgtwins {
		root := makeZOrderGraph(w, wins)
		level := filterGraphOverwrapping(root, w)

		curr := root
		for {
			curr = curr.Prev
			if curr == nil {
				break
			}

			setAlpha(curr.Window.Handle, alphaFromPercent(alpha, level))
			level--
		}
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

func runWatch(target string, interval time.Duration, alpha int, allprocs bool) error {
	fmt.Println("Press Ctrl+C to cancel.")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, os.Interrupt)

	var wins []*Window //save for listAllWindows() and recoverAlpha()
wachLoop:
	for {
		var err error
		wins, err = listAllWindows(allprocs, wins)
		if err != nil {
			return err
		}

		tgtwins := filterWindowsByTitle(wins, target)
		subtract := make([]*Window, 0, len(wins))
		copy(subtract, wins)

		for _, w := range tgtwins {
			root := makeZOrderGraph(w, wins)
			level := filterGraphOverwrapping(root, w)

			curr := root
			for {
				curr = curr.Prev
				if curr == nil {
					break
				}

				setAlpha(curr.Window.Handle, alphaFromPercent(alpha, level))

				idx := -1
				for i, s := range subtract {
					if s.Handle == curr.Window.Handle {
						idx = i
					}
				}
				if idx != -1 {
					subtract = append(subtract[:idx], subtract[idx+1:]...)
				}

				level--
			}
		}

		for _, w := range subtract {
			setAlpha(w.Handle, 255)
		}

		select {
		case <-signalChan:
			break wachLoop
		default:
		}

		time.Sleep(interval)
	}

	wins, _ = listAllWindows(allprocs, wins)
	recoverAlpha(wins)

	return nil
}

type WinNode struct {
	Window *Window
	Prev   *WinNode
}

func (n *WinNode) dump() string {
	if n == nil {
		return ""
	}

	return fmt.Sprintf("%v\n%v", n.Window.dump(), n.Prev.dump())
}

func (w *Window) dump() string {
	if w == nil {
		return ""
	}
	return fmt.Sprintf("%q(HWND=%v, PID=%v) %#v", w.Title, w.Handle, w.PID, w.Rect)
}

func makeZOrderGraph(tgt *Window, all []*Window) *WinNode {
	curr := &WinNode{Window: tgt, Prev: nil}
	concatPrevZOrderNode(curr, all)
	return curr
}

func concatPrevZOrderNode(curr *WinNode, all []*Window) {
	for _, w := range all {
		if w.Handle == curr.Window.ZPrevHandle {
			curr.Prev = &WinNode{Window: w, Prev: nil}
			concatPrevZOrderNode(curr.Prev, all)
		}
	}
}

func filterGraphOverwrapping(curr *WinNode, tgt *Window) int {
	if curr == nil || curr.Prev == nil {
		return 0
	}

	tr := tgt.Rect
	prev := curr.Prev

	isoverwrapping := prev.Window.Rect.Left <= tr.Right && tr.Left <= prev.Window.Rect.Right &&
		prev.Window.Rect.Top <= tr.Bottom && tr.Top <= prev.Window.Rect.Bottom
	isvisible, _, _ := isWindowVisible.Call(uintptr(prev.Window.Handle))

	if isoverwrapping && isvisible != 0 {
		// ok
		return filterGraphOverwrapping(prev, tgt) + 1
	} else {
		// cut
		curr.Prev = prev.Prev
		return filterGraphOverwrapping(curr, tgt)
	}
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
	if alpha == 255 {
		setLayeredWindowAttributes.Call(uintptr(hwnd), 0, 255, LWA_ALPHA)
		style, _, _ := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
		// clear WS_EX_LAYERED bit
		setWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE, style&^WS_EX_LAYERED)
	} else {
		style, _, _ := getWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE)
		setWindowLong.Call(uintptr(hwnd), GWL_EXSTYLE, style|WS_EX_LAYERED)
		setLayeredWindowAttributes.Call(uintptr(hwnd), 0, alpha, LWA_ALPHA)
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
			log.Printf("%q=>alpha=%v", w.Title, w.OrgAlpha)
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

func alphaFromPercent(percent, level int) uintptr {
	return uintptr(255 * math.Pow(float64(100-percent)/100, float64(level)))
}
