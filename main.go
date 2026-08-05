// fastKey - 按指定快捷键隐藏/恢复指定程序的窗口及任务栏图标
//
// 功能：
//   - 全局热键切换隐藏/恢复目标程序窗口（窗口 + 任务栏图标）
//   - 系统托盘图标，右键菜单：打开设置 / 退出
//   - 图形设置界面：热键捕获控件设置快捷键，从已安装/运行中的程序勾选目标
//
// 用法：
//   fastKey.exe            正常模式：托盘驻留，监听热键
//   fastKey.exe -settings  启动后直接打开设置窗口
//   fastKey.exe -test      自检模式：隐藏目标 3 秒后恢复并退出
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ---------------- Windows API ----------------

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procRegisterHotKey           = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey         = user32.NewProc("UnregisterHotKey")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procUpdateWindow             = user32.NewProc("UpdateWindow")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procLoadIconW                = user32.NewProc("LoadIconW")
	procLookupIconIdFromDirectoryEx = user32.NewProc("LookupIconIdFromDirectoryEx")
	procCreateIconFromResourceEx    = user32.NewProc("CreateIconFromResourceEx")
	procFindWindowExW               = user32.NewProc("FindWindowExW")
	procPostMessageW                = user32.NewProc("PostMessageW")
	procGetForegroundWindow         = user32.NewProc("GetForegroundWindow")
	procGetClassNameW               = user32.NewProc("GetClassNameW")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteExW  = shell32.NewProc("ShellExecuteExW")

	procGetStockObject = gdi32.NewProc("GetStockObject")

	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW             = kernel32.NewProc("CreateMutexW")
	procGetCurrentProcessId      = kernel32.NewProc("GetCurrentProcessId")
	procGetCurrentProcess        = kernel32.NewProc("GetCurrentProcess")
	procWaitForSingleObject      = kernel32.NewProc("WaitForSingleObject")
)

const (
	WM_HOTKEY   = 0x0312
	WM_COMMAND  = 0x0111
	WM_DESTROY  = 0x0002
	WM_CLOSE    = 0x0010
	WM_RBUTTONUP = 0x0205
	WM_SETFONT  = 0x0030

	MOD_ALT     = 0x0001
	MOD_CONTROL = 0x0002
	MOD_SHIFT   = 0x0004
	MOD_WIN     = 0x0008

	SW_HIDE = 0
	SW_SHOW = 5

	GWL_EXSTYLE      = -20
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_APPWINDOW  = 0x00040000

	GW_OWNER = 4

	SWP_NOSIZE       = 0x0001
	SWP_NOMOVE       = 0x0002
	SWP_NOZORDER     = 0x0004
	SWP_NOACTIVATE   = 0x0010
	SWP_FRAMECHANGED = 0x0020

	TH32CS_SNAPPROCESS = 0x00000002

	HOTKEY_ID_TOGGLE = 1
	HOTKEY_ID_EXIT   = 2

	HWND_MESSAGE = ^uintptr(2) // (HWND)-3

	// 托盘
	NIM_ADD    = 0x00000000
	NIM_DELETE = 0x00000002
	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	WM_TRAYICON = 0x8001 // WM_APP + 1
	TRAY_ICON_ID = 1

	TPM_BOTTOMALIGN = 0x0020
	TPM_LEFTALIGN   = 0x0000

	ID_TRAY_SETTINGS      = 2001
	ID_TRAY_EXIT          = 2002
	ID_TRAY_HIDE_FG       = 2003
	ID_TRAY_RESTART_ADMIN = 2004

	MF_STRING    = 0x0000
	MF_GRAYED    = 0x0001
	MF_SEPARATOR = 0x0800

	IDI_APPLICATION = 32512
)

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type POINT struct{ X, Y int32 }

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type processEntry32W struct {
	DwSize              uint32
	CntUsage            uint32
	Th32ProcessID       uint32
	Th32DefaultHeapID   uintptr
	Th32ModuleID        uint32
	CntThreads          uint32
	Th32ParentProcessID uint32
	PcPriClassBase      int32
	DwFlags             uint32
	SzExeFile           [260]uint16
}

//go:embed icon.ico
var iconData []byte

// ---------------- 配置 ----------------

type Target struct {
	Process       string `json:"process"`
	TitleContains string `json:"titleContains"`
}

type Config struct {
	Hotkey     string   `json:"hotkey"`
	ExitHotkey string   `json:"exitHotkey"`
	Targets    []Target `json:"targets"`
}

// currentCfg 当前生效配置（设置窗口保存时更新）
var currentCfg *Config

// hwndMain 隐藏消息窗口句柄（接收热键与托盘消息）
var hwndMain uintptr

func configPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("配置文件中 targets 为空")
	}
	if cfg.Hotkey == "" {
		return nil, fmt.Errorf("配置文件中 hotkey 为空")
	}
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0644)
}

// ---------------- 热键解析 ----------------

var specialKeys = map[string]uint32{
	"space": 0x20, "tab": 0x09, "esc": 0x1B, "escape": 0x1B,
	"enter": 0x0D, "return": 0x0D, "backspace": 0x08,
	"insert": 0x2D, "ins": 0x2D, "delete": 0x2E, "del": 0x2E,
	"home": 0x24, "end": 0x23, "pageup": 0x21, "pagedown": 0x22,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"printscreen": 0x2C, "pause": 0x13,
	"num0": 0x60, "num1": 0x61, "num2": 0x62, "num3": 0x63, "num4": 0x64,
	"num5": 0x65, "num6": 0x66, "num7": 0x67, "num8": 0x68, "num9": 0x69,
}

func vkName(vk uint32) string {
	for name, code := range specialKeys {
		if code == vk {
			return name
		}
	}
	switch {
	case vk >= 'A' && vk <= 'Z', vk >= '0' && vk <= '9':
		return string(rune(vk))
	case vk >= 0x70 && vk <= 0x87:
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	return fmt.Sprintf("0x%02X", vk)
}

// formatHotkey 生成 "Ctrl+Alt+H" 形式的展示字符串
func formatHotkey(mods, vk uint32) string {
	var parts []string
	if mods&MOD_CONTROL != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&MOD_ALT != 0 {
		parts = append(parts, "Alt")
	}
	if mods&MOD_SHIFT != 0 {
		parts = append(parts, "Shift")
	}
	if mods&MOD_WIN != 0 {
		parts = append(parts, "Win")
	}
	return strings.Join(append(parts, vkName(vk)), "+")
}

// parseHotkey 解析 "Ctrl+Alt+H" 形式的热键字符串
func parseHotkey(s string) (mods uint32, vk uint32, err error) {
	parts := strings.Split(s, "+")
	if len(parts) == 0 {
		return 0, 0, fmt.Errorf("热键为空")
	}
	for i, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			return 0, 0, fmt.Errorf("热键格式错误: %q", s)
		}
		switch p {
		case "ctrl", "control":
			mods |= MOD_CONTROL
		case "alt":
			mods |= MOD_ALT
		case "shift":
			mods |= MOD_SHIFT
		case "win":
			mods |= MOD_WIN
		default:
			if i != len(parts)-1 {
				return 0, 0, fmt.Errorf("无法识别的修饰键 %q（注意主键须放在最后）", p)
			}
			key, ok := specialKeys[p]
			if !ok {
				runes := []rune(p)
				if len(runes) == 1 {
					r := runes[0]
					switch {
					case r >= 'a' && r <= 'z':
						key = uint32(r - 'a' + 'A')
					case r >= '0' && r <= '9':
						key = uint32(r)
					default:
						return 0, 0, fmt.Errorf("不支持的按键 %q", p)
					}
				} else if len(p) >= 2 && p[0] == 'f' {
					var n int
					if _, e := fmt.Sscanf(p[1:], "%d", &n); e != nil || n < 1 || n > 24 {
						return 0, 0, fmt.Errorf("不支持的功能键 %q", p)
					}
					key = 0x70 + uint32(n) - 1
				} else {
					return 0, 0, fmt.Errorf("不支持的按键 %q", p)
				}
			}
			vk = key
		}
	}
	if vk == 0 {
		return 0, 0, fmt.Errorf("热键 %q 缺少主键", s)
	}
	return mods, vk, nil
}

// ---------------- 热键注册（绑定到隐藏窗口） ----------------

func unregisterHotkeys() {
	if hwndMain != 0 {
		procUnregisterHotKey.Call(hwndMain, HOTKEY_ID_TOGGLE)
		procUnregisterHotKey.Call(hwndMain, HOTKEY_ID_EXIT)
	}
}

// applyHotkeys 按当前配置注册热键，返回错误信息（若有）
func applyHotkeys(cfg *Config) error {
	unregisterHotkeys()
	mods, vk, err := parseHotkey(cfg.Hotkey)
	if err != nil {
		return fmt.Errorf("隐藏/恢复热键: %w", err)
	}
	if r, _, e := procRegisterHotKey.Call(hwndMain, HOTKEY_ID_TOGGLE, uintptr(mods), uintptr(vk)); r == 0 {
		return fmt.Errorf("注册隐藏/恢复热键失败（可能被占用）: %v", e)
	}
	if cfg.ExitHotkey != "" {
		if em, evk, e := parseHotkey(cfg.ExitHotkey); e == nil {
			procRegisterHotKey.Call(hwndMain, HOTKEY_ID_EXIT, uintptr(em), uintptr(evk))
		}
	}
	return nil
}

// ---------------- 进程快照 ----------------

func snapshotProcesses() map[uint32]string {
	result := make(map[uint32]string)
	snap, _, _ := procCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == ^uintptr(0) {
		return result
	}
	defer procCloseHandle.Call(snap)

	var entry processEntry32W
	entry.DwSize = uint32(unsafe.Sizeof(entry))
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for r != 0 {
		name := strings.ToLower(syscall.UTF16ToString(entry.SzExeFile[:]))
		result[entry.Th32ProcessID] = name
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return result
}

// ---------------- 窗口操作 ----------------

func windowTitle(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

// findTargetWindows 枚举顶层窗口，返回匹配目标且当前可见的窗口。
// 进程名匹配：包含该进程所有可见窗口（含浮动面板，排除 IME 辅助窗口）；
// 标题匹配：仅无属主的主窗口，避免误伤同标题对话框。
func findTargetWindows(cfg *Config) []uintptr {
	procNames := snapshotProcesses()
	var matched []uintptr

	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		owner, _, _ := procGetWindow.Call(hwnd, GW_OWNER)
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		exeName := procNames[pid]
		title := windowTitle(hwnd)

		for _, t := range cfg.Targets {
			match := false
			if t.Process != "" && exeName != "" && exeName == strings.ToLower(t.Process) {
				// 排除输入法辅助窗口
				cls := windowClassName(hwnd)
				if cls != "IME" && cls != "MSCTFIME UI" {
					match = true
				}
			}
			if !match && owner == 0 && t.TitleContains != "" && title != "" &&
				strings.Contains(strings.ToLower(title), strings.ToLower(t.TitleContains)) {
				match = true
			}
			if match && isWindowVisible(hwnd) {
				matched = append(matched, hwnd)
			}
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return matched
}

// ---------------- 隐藏 / 恢复 ----------------

var hiddenWindows = map[uintptr]int64{}

// hideWindow 隐藏窗口；跨权限失败（目标以管理员运行）时返回错误
func hideWindow(hwnd uintptr) error {
	exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(0xFFFFFFEC))
	orig := int64(exStyle)
	newStyle := (orig &^ WS_EX_APPWINDOW) | WS_EX_TOOLWINDOW
	r, _, e := procSetWindowLongPtrW.Call(hwnd, uintptr(0xFFFFFFEC), uintptr(int64(newStyle)))
	if r == 0 && e != syscall.Errno(0) {
		return fmt.Errorf("无权限操作（错误码 %d）", e)
	}
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED)
	procShowWindow.Call(hwnd, SW_HIDE)
	// 校验是否真的隐藏了（高权限窗口 ShowWindow 会静默失败）
	if isWindowVisible(hwnd) {
		// 回滚样式
		procSetWindowLongPtrW.Call(hwnd, uintptr(0xFFFFFFEC), uintptr(orig))
		return fmt.Errorf("隐藏未生效（目标可能以管理员权限运行）")
	}
	hiddenWindows[hwnd] = orig
	return nil
}

func restoreWindow(hwnd uintptr, origExStyle int64) {
	procSetWindowLongPtrW.Call(hwnd, uintptr(0xFFFFFFEC), uintptr(origExStyle))
	procShowWindow.Call(hwnd, SW_SHOW)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_FRAMECHANGED)
	procSetForegroundWindow.Call(hwnd)
}

func restoreAll() int {
	count := 0
	for hwnd, style := range hiddenWindows {
		if r, _, _ := procIsWindow.Call(hwnd); r != 0 {
			restoreWindow(hwnd, style)
			count++
		}
	}
	hiddenWindows = map[uintptr]int64{}
	return count
}

func toggle(cfg *Config) string {
	if len(hiddenWindows) > 0 {
		n := restoreAll()
		return fmt.Sprintf("已恢复 %d 个窗口", n)
	}
	targets := findTargetWindows(cfg)
	if len(targets) == 0 {
		return "未找到匹配的目标窗口（程序未运行或进程名/标题不匹配）"
	}
	var ok, failed int
	for _, hwnd := range targets {
		if err := hideWindow(hwnd); err != nil {
			failed++
			logf("隐藏窗口失败: %v", err)
		} else {
			ok++
		}
	}
	msg := fmt.Sprintf("已隐藏 %d 个窗口", ok)
	if failed > 0 {
		msg += fmt.Sprintf("，%d 个失败（目标以管理员权限运行）", failed)
		if !isElevated() {
			msg += "——请右键托盘选择“以管理员身份重启”"
		}
	}
	return msg
}

// ---------------- 权限检测与提权重启 ----------------

// isElevated 检测当前进程是否以管理员权限运行
func isElevated() bool {
	var token uintptr
	cur, _, _ := procGetCurrentProcess.Call()
	r, _, _ := procOpenProcessToken.Call(cur, 0x0008 /*TOKEN_QUERY*/, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return false
	}
	defer procCloseHandle.Call(token)
	var elevation uint32
	var size uint32
	// TokenElevation = 20
	r, _, _ = procGetTokenInformation.Call(token, 20, uintptr(unsafe.Pointer(&elevation)), 4,
		uintptr(unsafe.Pointer(&size)))
	return r != 0 && elevation != 0
}

// relaunchElevated 以管理员身份重新启动自身（弹出 UAC），成功后当前实例应退出
func relaunchElevated() bool {
	return shellExecuteRunas("")
}

// runElevatedHelper 以管理员身份运行自身的辅助命令并等待其结束（UAC 期间阻塞等待，最长 60 秒）
func runElevatedHelper(args string) bool {
	return shellExecuteRunas(args)
}

func shellExecuteRunas(args string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	var params *uint16
	if args != "" {
		params, _ = syscall.UTF16PtrFromString(args)
	}
	var sei SHELLEXECUTEINFOW
	sei.CbSize = uint32(unsafe.Sizeof(sei))
	sei.FMask = 0x00000040 // SEE_MASK_NOCLOSEPROCESS
	sei.LpVerb = verb
	sei.LpFile = file
	sei.LpParameters = params
	sei.NShow = 1 // SW_NORMAL
	r, _, _ := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		return false
	}
	if sei.HProcess != 0 {
		procWaitForSingleObject.Call(sei.HProcess, 60000)
		procCloseHandle.Call(sei.HProcess)
	}
	return true
}

type SHELLEXECUTEINFOW struct {
	CbSize       uint32
	FMask        uint32
	Hwnd         uintptr
	LpVerb       *uint16
	LpFile       *uint16
	LpParameters *uint16
	LpDirectory  *uint16
	NShow        int32
	HInstApp     uintptr
	LpIDList     uintptr
	LpClass      *uint16
	HkeyClass    uintptr
	DwHotKey     uint32
	HIcon        uintptr
	HProcess     uintptr
}

// ---------------- 托盘图标 ----------------

// loadAppIcon 从嵌入的 ico 数据创建图标，失败则用系统默认
func loadAppIcon() uintptr {
	if len(iconData) > 0 {
		h, _, _ := procCreateIconFromResourceEx.Call(
			uintptr(unsafe.Pointer(&iconData[0])), uintptr(len(iconData)),
			1, 0x00030000, 32, 32, 0)
		if h != 0 {
			return h
		}
	}
	h, _, _ := procLoadIconW.Call(0, IDI_APPLICATION)
	return h
}

var trayIconAdded bool

func addTrayIcon(hwnd uintptr) {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = TRAY_ICON_ID
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = loadAppIcon()
	tip := "fastKey - " + currentCfg.Hotkey
	copy(nid.SzTip[:], syscall.StringToUTF16(tip))
	if r, _, _ := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid))); r != 0 {
		trayIconAdded = true
		logf("托盘图标已添加")
	} else {
		logf("托盘图标添加失败")
	}
}

func removeTrayIcon(hwnd uintptr) {
	if !trayIconAdded {
		return
	}
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = TRAY_ICON_ID
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	trayIconAdded = false
}

func showTrayMenu(hwnd uintptr) {
	// 弹出菜单前捕获前台窗口（菜单弹出后焦点会被抢走）
	pendingForeground = validForegroundWindow(getForegroundWindow())

	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	// 第一项：隐藏当前活动窗口（带窗口标题，无可隐藏目标时置灰）
	hideText := "隐藏当前活动窗口(&H)"
	hideFlags := uintptr(MF_STRING)
	if pendingForeground != 0 {
		title := windowTitle(pendingForeground)
		runes := []rune(title)
		if len(runes) > 16 {
			title = string(runes[:16]) + "…"
		}
		hideText = fmt.Sprintf("隐藏「%s」(&H)", title)
	} else {
		hideFlags |= MF_GRAYED
	}
	s0, _ := syscall.UTF16PtrFromString(hideText)
	procAppendMenuW.Call(menu, hideFlags, ID_TRAY_HIDE_FG, uintptr(unsafe.Pointer(s0)))
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)

	s1, _ := syscall.UTF16PtrFromString("设置(&S)...")
	s2, _ := syscall.UTF16PtrFromString("退出(&X)")
	procAppendMenuW.Call(menu, 0, ID_TRAY_SETTINGS, uintptr(unsafe.Pointer(s1)))
	if !isElevated() {
		s3, _ := syscall.UTF16PtrFromString("以管理员身份重启（可隐藏管理员程序）(&A)")
		procAppendMenuW.Call(menu, 0, ID_TRAY_RESTART_ADMIN, uintptr(unsafe.Pointer(s3)))
	}
	procAppendMenuW.Call(menu, 0, ID_TRAY_EXIT, uintptr(unsafe.Pointer(s2)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 需要先置前，菜单点击外部才能正常关闭
	procSetForegroundWindow.Call(hwnd)
	procTrackPopupMenu.Call(menu, TPM_LEFTALIGN|TPM_BOTTOMALIGN,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procDestroyMenu.Call(menu)
}

// ---------------- 临时隐藏当前活动窗口 ----------------

// pendingForeground 弹托盘菜单时捕获的前台窗口
var pendingForeground uintptr

// 不可隐藏的桌面/外壳窗口类名
var excludedClasses = map[string]bool{
	"Progman":                true,
	"WorkerW":                true,
	"Shell_TrayWnd":          true,
	"Shell_SecondaryTrayWnd": true,
	"DV2ControlHost":         true,
	"XamlExplorerHostIslandWindow": true,
}

func getForegroundWindow() uintptr {
	h, _, _ := procGetForegroundWindow.Call()
	return h
}

func windowClassName(hwnd uintptr) string {
	buf := make([]uint16, 128)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// validForegroundWindow 校验窗口是否可作为"当前活动窗口"被隐藏，不可则返回 0
func validForegroundWindow(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	if r, _, _ := procIsWindow.Call(hwnd); r == 0 {
		return 0
	}
	// 排除本进程自己的窗口
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	myPid, _, _ := procGetCurrentProcessId.Call()
	if pid == uint32(myPid) {
		return 0
	}
	// 排除桌面/任务栏等外壳窗口
	if excludedClasses[windowClassName(hwnd)] {
		return 0
	}
	if !isWindowVisible(hwnd) || windowTitle(hwnd) == "" {
		return 0
	}
	return hwnd
}

// hideForegroundWindow 隐藏 pendingForeground（无效时退回实时获取的前台窗口）
func hideForegroundWindow() string {
	target := pendingForeground
	pendingForeground = 0
	if validForegroundWindow(target) == 0 {
		target = validForegroundWindow(getForegroundWindow())
	}
	if target == 0 {
		return "没有可隐藏的活动窗口"
	}
	if _, hidden := hiddenWindows[target]; hidden {
		return "该窗口已在隐藏列表中"
	}
	title := windowTitle(target)
	if err := hideWindow(target); err != nil {
		return fmt.Sprintf("隐藏「%s」失败：%v", title, err)
	}
	return fmt.Sprintf("已隐藏活动窗口「%s」（按热键可恢复）", title)
}

// ---------------- 主窗口过程 ----------------

func mainWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_HOTKEY:
		switch wParam {
		case HOTKEY_ID_TOGGLE:
			logf("%s", toggle(currentCfg))
		case HOTKEY_ID_EXIT:
			quitApp(hwnd)
		}
		return 0
	case WM_TRAYICON:
		if lParam == WM_RBUTTONUP {
			showTrayMenu(hwnd)
		}
		return 0
	case WM_COMMAND:
		switch wParam & 0xFFFF {
		case ID_TRAY_SETTINGS:
			showSettingsWindow()
		case ID_TRAY_HIDE_FG:
			logf("%s", hideForegroundWindow())
		case ID_TRAY_RESTART_ADMIN:
			if relaunchElevated() {
				quitApp(hwnd)
			} else {
				msgbox(hwnd, "fastKey", "提权启动失败（可能取消了 UAC 提示）。", 0x30)
			}
		case ID_TRAY_EXIT:
			quitApp(hwnd)
		}
		return 0
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func quitApp(hwnd uintptr) {
	if n := restoreAll(); n > 0 {
		logf("退出前已恢复 %d 个窗口", n)
	}
	removeTrayIcon(hwnd)
	procDestroyWindow.Call(hwnd)
}

// ---------------- 主程序 ----------------

func main() {
	// 窗口、热键、消息循环都绑定线程，必须锁定 OS 线程
	runtime.LockOSThread()

	// 自检模式
	if len(os.Args) > 1 && os.Args[1] == "-test" {
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		runSelfTest(cfg)
		return
	}

	// 提权辅助模式：由 runElevatedHelper 以管理员身份调用，执行后退出
	if len(os.Args) > 1 && (os.Args[1] == "-admin-autostart-on" || os.Args[1] == "-admin-autostart-off") {
		if err := setAdminAutostart(os.Args[1] == "-admin-autostart-on"); err != nil {
			fmt.Fprintln(os.Stderr, "设置管理员自启动失败:", err)
			os.Exit(1)
		}
		return
	}

	openSettings := len(os.Args) > 1 && os.Args[1] == "-settings"

	// 单实例检查：已有实例运行时，若是 -settings 则通知已有实例打开设置，随后退出
	mutexName, _ := syscall.UTF16PtrFromString("fastKeySingleInstanceMutex")
	hMutex, _, errNo := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	_ = hMutex // 保持句柄存活，进程退出时自动释放
	if errNo == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		if openSettings {
			cls, _ := syscall.UTF16PtrFromString("fastKeyMainWnd")
			if h, _, _ := procFindWindowExW.Call(HWND_MESSAGE, 0, uintptr(unsafe.Pointer(cls)), 0); h != 0 {
				procPostMessageW.Call(h, WM_COMMAND, ID_TRAY_SETTINGS, 0)
			}
		}
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		// 配置缺失/损坏时给一份默认配置，让用户进设置界面配置
		cfg = &Config{
			Hotkey:     "Ctrl+Alt+H",
			ExitHotkey: "Ctrl+Alt+Q",
			Targets:    []Target{{Process: "notepad.exe"}},
		}
		openSettings = true
	}
	currentCfg = cfg

	hInstance, _, _ := procGetModuleHandleW.Call(0)

	className, _ := syscall.UTF16PtrFromString("fastKeyMainWnd")
	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(mainWndProc)
	wc.HInstance = hInstance
	wc.LpszClassName = className
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := syscall.UTF16PtrFromString("fastKey")
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0,
		HWND_MESSAGE, 0, hInstance, 0)
	if hwnd == 0 {
		fmt.Fprintln(os.Stderr, "创建消息窗口失败")
		os.Exit(1)
	}
	hwndMain = hwnd

	if err := applyHotkeys(cfg); err != nil {
		msgbox(0, "fastKey", err.Error()+"\n请通过托盘菜单打开设置修改快捷键。", 0x30)
	}

	addTrayIcon(hwnd)
	if isElevated() {
		logf("fastKey 已启动（热键 %s，管理员权限）", cfg.Hotkey)
	} else {
		logf("fastKey 已启动（热键 %s，普通权限）", cfg.Hotkey)
	}

	if openSettings {
		showSettingsWindow()
	}

	// 消息循环
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 || r == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	removeTrayIcon(hwnd)
	logf("fastKey 已退出")
}

// msgbox 弹消息框
func msgbox(owner uintptr, caption, text string, flags uint32) {
	c, _ := syscall.UTF16PtrFromString(caption)
	t, _ := syscall.UTF16PtrFromString(text)
	procMessageBoxW.Call(owner, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(flags))
}

// runSelfTest 自检：列出目标 → 隐藏 → 验证不可见 → 恢复 → 验证可见
func runSelfTest(cfg *Config) {
	fmt.Println("[自检] 查找目标窗口...")
	targets := findTargetWindows(cfg)
	if len(targets) == 0 {
		fmt.Println("[自检] 失败：未找到目标窗口，请先启动目标程序")
		os.Exit(2)
	}
	fmt.Printf("[自检] 找到 %d 个目标窗口\n", len(targets))

	var hideFailed int
	for _, hwnd := range targets {
		if err := hideWindow(hwnd); err != nil {
			hideFailed++
			fmt.Printf("[自检] 隐藏窗口失败: %v\n", err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	allHidden := true
	for _, hwnd := range targets {
		if isWindowVisible(hwnd) {
			allHidden = false
		}
	}
	if allHidden {
		fmt.Println("[自检] 隐藏后验证：所有目标窗口均不可见 ✓")
	} else {
		fmt.Println("[自检] 隐藏后验证：仍有窗口可见 ✗")
	}

	fmt.Println("[自检] 3 秒后恢复...")
	time.Sleep(3 * time.Second)
	n := restoreAll()
	time.Sleep(500 * time.Millisecond)
	allVisible := true
	for _, hwnd := range targets {
		if r, _, _ := procIsWindow.Call(hwnd); r != 0 && !isWindowVisible(hwnd) {
			allVisible = false
		}
	}
	fmt.Printf("[自检] 已恢复 %d 个窗口\n", n)
	if allVisible {
		fmt.Println("[自检] 恢复后验证：所有目标窗口均可见 ✓")
	} else {
		fmt.Println("[自检] 恢复后验证：存在不可见窗口 ✗")
	}
	if allHidden && allVisible {
		fmt.Println("[自检] 通过")
	} else {
		os.Exit(3)
	}
}

func logf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	fmt.Println(line)
	exe, err := os.Executable()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(exe), "fastKey.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}
