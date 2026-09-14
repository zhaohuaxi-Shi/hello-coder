//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/hello-coder/hello-coder/internal/version"
	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func Supported() bool { return true }

func Open(url, dataDir string) error {
	profile, err := filepath.Abs(filepath.Join(dataDir, "ui-profile"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return err
	}

	hideOwnConsole()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  profile,
		WindowOptions: webview2.WindowOptions{
			Title:  "Hello Coder " + version.Short(),
			Width:  1400,
			Height: 900,
			Center: true,
		},
	})
	if w == nil {
		return openChromiumApp(url, profile)
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())
	applyDarkFrame(hwnd)
	setWindowIcon(hwnd)
	w.Navigate(url)
	w.Run()
	return nil
}

const (
	dwmwaUseImmersiveDarkModeOld = 19
	dwmwaUseImmersiveDarkMode    = 20
	dwmwaBorderColor             = 34
	dwmwaCaptionColor            = 35
	dwmwaTextColor               = 36
)

func applyDarkFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	enableAppDarkMode()
	allowDarkModeForWindow(hwnd)

	dwmapi := windows.NewLazySystemDLL("dwmapi.dll")
	setAttr := dwmapi.NewProc("DwmSetWindowAttribute")
	if err := setAttr.Find(); err != nil {
		return
	}

	dark := int32(1)
	_, _, _ = setAttr.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	_, _, _ = setAttr.Call(hwnd, dwmwaUseImmersiveDarkModeOld, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))

	black := uint32(0x000000)
	white := uint32(0x00FFFFFF)
	_, _, _ = setAttr.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&black)), 4)
	_, _, _ = setAttr.Call(hwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&black)), 4)
	_, _, _ = setAttr.Call(hwnd, dwmwaTextColor, uintptr(unsafe.Pointer(&white)), 4)

	const (
		swpNoSize       = 0x0001
		swpNoMove       = 0x0002
		swpNoZOrder     = 0x0004
		swpFrameChanged = 0x0020
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	_, _, _ = user32.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoZOrder|swpFrameChanged)
}

func enableAppDarkMode() {
	uxtheme := windows.NewLazySystemDLL("uxtheme.dll")
	if proc := uxtheme.NewProc("#135"); proc.Find() == nil {
		const forceDark = 2
		_, _, _ = proc.Call(forceDark)
	}
	if proc := uxtheme.NewProc("#136"); proc.Find() == nil {
		_, _, _ = proc.Call()
	}
}

func allowDarkModeForWindow(hwnd uintptr) {
	uxtheme := windows.NewLazySystemDLL("uxtheme.dll")
	if proc := uxtheme.NewProc("#133"); proc.Find() == nil {
		_, _, _ = proc.Call(hwnd, 1)
	}
}

func setWindowIcon(hwnd uintptr) {
	if hwnd == 0 || len(appIcon) == 0 {
		return
	}
	tmp := filepath.Join(os.TempDir(), "hello-coder-app.ico")
	if err := os.WriteFile(tmp, appIcon, 0o644); err != nil {
		return
	}
	path16, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	loadImage := user32.NewProc("LoadImageW")
	send := user32.NewProc("SendMessageW")
	getMetrics := user32.NewProc("GetSystemMetrics")
	setClassLong := user32.NewProc("SetClassLongPtrW")
	const (
		imageIcon      = 1
		lrLoadFromFile = 0x0010
		wmSetIcon      = 0x0080
		iconSmall      = 0
		iconBig        = 1
		smCxIcon       = 11
		smCyIcon       = 12
		smCxSmIcon     = 49
		smCySmIcon     = 50
		gclpHicon      = ^uintptr(13) // -14
		gclpHiconSm    = ^uintptr(33) // -34
	)
	cxBig, _, _ := getMetrics.Call(smCxIcon)
	cyBig, _, _ := getMetrics.Call(smCyIcon)
	cxSm, _, _ := getMetrics.Call(smCxSmIcon)
	cySm, _, _ := getMetrics.Call(smCySmIcon)
	hBig, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(path16)), imageIcon, cxBig, cyBig, lrLoadFromFile)
	hSm, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(path16)), imageIcon, cxSm, cySm, lrLoadFromFile)
	if hSm != 0 {
		_, _, _ = send.Call(hwnd, wmSetIcon, iconSmall, hSm)
		_, _, _ = setClassLong.Call(hwnd, gclpHiconSm, hSm)
	}
	if hBig != 0 {
		_, _, _ = send.Call(hwnd, wmSetIcon, iconBig, hBig)
		_, _, _ = setClassLong.Call(hwnd, gclpHicon, hBig)
	}
}

func notifyError(title, text string) {
	_, _ = windows.MessageBox(0, windows.StringToUTF16Ptr(text), windows.StringToUTF16Ptr(title), windows.MB_OK|windows.MB_ICONERROR)
}

func hideOwnConsole() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	user32 := windows.NewLazySystemDLL("user32.dll")
	showWindow := user32.NewProc("ShowWindow")

	var pids [8]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 8)
	if n != 1 {
		return
	}
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	const swHide = 0
	_, _, _ = showWindow.Call(hwnd, swHide)
}

func openChromiumApp(url, profile string) error {
	bin, err := findChromium()
	if err != nil {
		return fmt.Errorf("create window failed (install WebView2 runtime or Edge/Chrome): %w", err)
	}
	cmd := exec.Command(bin,
		"--app="+url,
		"--user-data-dir="+profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-extensions",
		"--window-size=1400,900",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open window: %w", err)
	}
	return cmd.Wait()
}

func findChromium() (string, error) {
	for _, name := range []string{"msedge.exe", "chrome.exe"} {
		if p := lookInRegistry(name); p != "" {
			return p, nil
		}
	}
	var roots []string
	for _, env := range []string{"PROGRAMFILES(X86)", "PROGRAMFILES", "LOCALAPPDATA"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}
	candidates := []string{
		filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
	}
	for _, root := range roots {
		for _, rel := range candidates {
			p := filepath.Join(root, rel)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("msedge/chrome not found")
}

func lookInRegistry(name string) string {
	sub := `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + name
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, err := k.GetStringValue("")
		_ = k.Close()
		if err != nil || val == "" {
			continue
		}
		if st, err := os.Stat(val); err == nil && !st.IsDir() {
			return val
		}
	}
	return ""
}
