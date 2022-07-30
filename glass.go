package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/shu-go/buildcond/cond"
	"github.com/shu-go/gli"
	"github.com/shu-go/retry"
	"github.com/shu-go/rog"
)

// Version is app version
var Version string

func init() {
	if Version == "" {
		Version = "dev-" + time.Now().Format("20060102")
	}
}

var (
	verbose = rog.Discard
	//verbose = log.New(ioutil.Discard)
)

type globalCmd struct {
	Watch   watchCmd   `cli:"watch, w" help:"run constantly"`
	List    listCmd    `cli:"list, ls" help:"list overwrapping windows"`
	Temp    tempCmd    `help:"run once"`
	Recover recoverCmd `help:"force all windows untransparent"`

	Verbose bool `cli:"verbose, v" help:"verbose output to stderr"`
}

func (g globalCmd) Before() {
	if g.Verbose {
		verbose = rog.New(os.Stderr, "", 0)
	}
	cond.IfDebug(func() {
		rog.EnableDebug()
	})
}

func main() {
	app := gli.NewWith(&globalCmd{})
	app.Name = "glass"
	app.Desc = "make overwrapping windows be transparent"
	app.Version = Version
	app.Run(os.Args)
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
		Title        string
		Handle       syscall.Handle
		PID          int
		ZPrevHandle  syscall.Handle
		Rect         Rect
		OrgAlpha     int
		ColorProfile ColorProfile
	}
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	enumWindows                = user32.NewProc("EnumWindows")
	getForegroundWindow        = user32.NewProc("GetForegroundWindow")
	getLayeredWindowAttributes = user32.NewProc("GetLayeredWindowAttributes")
	getWindow                  = user32.NewProc("GetWindow")
	getWindowDC                = user32.NewProc("GetWindowDC")
	getWindowLong              = user32.NewProc("GetWindowLongW")
	getWindowRect              = user32.NewProc("GetWindowRect")
	getWindowText              = user32.NewProc("GetWindowTextW")
	getWindowTextLength        = user32.NewProc("GetWindowTextLengthW")
	getWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	isIconic                   = user32.NewProc("IsIconic")
	isWindow                   = user32.NewProc("IsWindow")
	isWindowVisible            = user32.NewProc("IsWindowVisible")
	releaseDC                  = user32.NewProc("ReleaseDC")
	setLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	setWindowLong              = user32.NewProc("SetWindowLongW")

	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	getPixel = gdi32.NewProc("GetPixel")
)

var gCallback uintptr
var gCallbackOnce sync.Once
var gPtrOrgDict *map[syscall.Handle]*Window
var gPtrwins *[]*Window

func listAllWindows(orgWins []*Window) ([]*Window, error) {
	orgDict := makeHWND2WindowDict(orgWins)
	gPtrOrgDict = &orgDict
	gPtrwins = &([]*Window{})

	gCallbackOnce.Do(func() {
		gCallback = syscall.NewCallback(func(hwnd syscall.Handle, lparam uintptr) uintptr {
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
			orgDict := *gPtrOrgDict
			if orgDict != nil {
				if w, found := orgDict[hwnd]; found {
					orgWin = w
				}
			}

			var colorProfile ColorProfile
			if (orgWin == nil || orgWin.ColorProfile == nil) && (r.Left != r.Right && r.Top != r.Bottom) {
				d := 3
				colorProfile = make(ColorProfile, 0, d)

				dx := (r.Right - r.Left) / (int32(d) + 1)
				dy := (r.Bottom - r.Top) / (int32(d) + 1)

				hdc, _, _ := getWindowDC.Call(uintptr(hwnd))

				x := dx
				y := dy
				for i := 0; i < d; i++ {
					colorref, _, _ := getPixel.Call(hdc, uintptr(x), uintptr(y))
					c := NewColor(colorref)
					if c != nil {
						colorProfile = append(colorProfile, c)
					}

					x += dx
					y += dy
				}
				releaseDC.Call(uintptr(hwnd), hdc)
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
			if len(colorProfile) > 0 {
				win.ColorProfile = colorProfile
			}
			*gPtrwins = append(*gPtrwins, win)

			return 1
		})
	})

	a, _, _ := enumWindows.Call(gCallback, 0)
	if a == 0 {
		return nil, fmt.Errorf("USER32.EnumWindows returned FALSE")
	}

	return *gPtrwins, nil
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

func alphaFromPercent(percent, level int, curve float64, gray uint8) uintptr {
	//return uintptr(255 * math.Pow(float64(100-percent)/100, math.Pow(float64(level), curve)))
	v := math.Pow(float64(100-percent)/100, math.Pow(float64(level), curve))

	// gray ==   0 => x1.0
	// gray == 128 => x1.0 --+
	// gray == 255 => x0.5 --+-- liner
	if gray > 128 {
		v *= ((0.5-1.0)/(255-128))*float64(gray) + 1.5
	}

	v = math.Min(1.0, v)

	rog.Print(uintptr(255*math.Pow(float64(100-percent)/100, math.Pow(float64(level), curve))), uintptr(255*v))

	return uintptr(255 * v)
}

type ColorProfile []*Color

type Color struct {
	COLORREF uintptr

	Gray uint8

	H uint16 // [0, 360]
	S uint8  // [0, 255]
	V uint8  // [0, 255]
}

func NewColor(colorref uintptr) *Color {
	if colorref == 0xffffffff {
		return nil
	}
	if colorref == 0 {
		colorref = 0x00ffffff
	}

	r := int16(colorref & 0xff)
	g := int16((colorref << 2) & 0xff)
	b := int16((colorref << 4) & 0xff)

	min := r
	if min < g {
		min = g
	}
	if min < b {
		min = b
	}

	max := r
	if max > g {
		max = g
	}
	if max > b {
		max = b
	}

	var gray uint8
	gray = uint8(float64(r)*0.3 + float64(g)*0.59 + float64(b)*0.11)

	var h int16
	var s, v uint8

	if min == max {
		h = 0
	} else if min == b {
		h = 60*(g-b)/(max-min) + 60
	} else if min == r {
		h = 60*(b-g)/(max-min) + 180
	} else {
		h = 60*(r-b)/(max-min) + 300
	}
	if h < 0 {
		h += 360
	}
	s = uint8((max - min))
	v = uint8(max)

	return &Color{
		COLORREF: colorref,
		Gray:     gray,
		H:        uint16(h),
		S:        s,
		V:        v,
	}
}

func (c *Color) IsValid() bool {
	return c != nil && c.COLORREF != 0xffffffff
}

func (cp ColorProfile) AvgGray(defaultValue uint8) uint8 {
	if len(cp) == 0 {
		return defaultValue
	}

	count := 0
	avg := 0

	for _, c := range cp {
		if c.IsValid() {
			count++
			avg += int(c.Gray)
		}
	}

	if count == 0 {
		return defaultValue
	}

	return uint8(avg / count)
}

func (cp ColorProfile) String() string {
	s := "["
	for i, c := range cp {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%x", c.COLORREF)
	}
	s += "]"

	return s
}
