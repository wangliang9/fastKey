// gui.go - fastKey 图形设置界面
//
// 原生 Win32 控件实现：
//   - 带复选框的 ListView：列出已安装程序（注册表 Uninstall）与正在运行的程序
//   - 系统热键捕获控件 msctls_hotkey32：直接按下组合键完成设置
//   - 保存后写入 config.json 并立即重新注册热键，无需重启
package main

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")

	procGetDlgItem = user32.NewProc("GetDlgItem")
	procSetFocus   = user32.NewProc("SetFocus")
)

const (
	WS_CHILD        = 0x40000000
	WS_VISIBLE      = 0x10000000
	WS_BORDER       = 0x00800000
	WS_TABSTOP      = 0x00010000
	WS_CAPTION      = 0x00C00000
	WS_SYSMENU      = 0x00080000
	WS_VSCROLL      = 0x00200000
	WS_EX_CLIENTEDGE = 0x00000200

	BS_PUSHBUTTON    = 0x00000000
	BS_AUTOCHECKBOX  = 0x00000003
	BM_GETCHECK      = 0x00F0
	BM_SETCHECK      = 0x00F1
	BST_CHECKED      = 0x0001
	ES_AUTOHSCROLL   = 0x00000080

	LVS_REPORT        = 0x0001
	LVS_SINGLESEL     = 0x0004
	LVS_SHOWSELALWAYS = 0x0008
	LVS_SORTASCENDING = 0x0010

	LVS_EX_CHECKBOXES    = 0x00000004
	LVS_EX_FULLROWSELECT = 0x00000020

	LVM_FIRST                 = 0x1000
	LVM_SETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 54
	LVM_INSERTCOLUMNW         = LVM_FIRST + 97
	LVM_INSERTITEMW           = LVM_FIRST + 77
	LVM_SETITEMSTATE          = LVM_FIRST + 43
	LVM_GETITEMSTATE          = LVM_FIRST + 44
	LVM_GETITEMCOUNT          = LVM_FIRST + 4

	LVCF_WIDTH   = 0x0002
	LVCF_SUBITEM = 0x0008
	LVCF_TEXT    = 0x0004

	LVIF_TEXT  = 0x00000001
	LVIF_STATE = 0x00000008

	LVIS_STATEIMAGEMASK = 0xF000

	HKM_SETHOTKEY = 0x0401
	HKM_GETHOTKEY = 0x0402

	HOTKEYF_SHIFT   = 0x01
	HOTKEYF_CONTROL = 0x02
	HOTKEYF_ALT     = 0x04

	ICC_LISTVIEW_CLASSES = 0x00000001
	ICC_HOTKEY_CLASS     = 0x00000040

	DEFAULT_GUI_FONT = 17

	IDC_LIST         = 1001
	IDC_EDIT_MANUAL  = 1002
	IDC_BTN_ADD      = 1003
	IDC_BTN_REFRESH  = 1004
	IDC_HOTKEY_MAIN  = 1005
	IDC_HOTKEY_EXIT  = 1006
	IDC_CHK_AUTOSTART = 1007
	IDC_CHK_AUTOSTART_ADMIN = 1008
	IDC_BTN_SAVE     = 1010
	IDC_BTN_CANCEL   = 1011
)

type INITCOMMONCONTROLSEX struct {
	DwSize uint32
	DwICC  uint32
}

type LVCOLUMNW struct {
	Mask       uint32
	Fmt        int32
	Cx         int32
	PszText    *uint16
	CchTextMax int32
	ISubItem   int32
	IImage     int32
	IOrder     int32
	CxMin      int32
	CxDefault  int32
	CxIdeal    int32
}

type LVITEMW struct {
	Mask       uint32
	IItem      int32
	ISubItem   int32
	State      uint32
	StateMask  uint32
	PszText    *uint16
	CchTextMax int32
	IImage     int32
	LParam     uintptr
	IIndent    int32
	IGroupId   int32
	CColumns   uint32
	PuColumns  *uint32
	PiColFmt   *int32
	IGroup     int32
}

// progEntry 一条可选程序项
type progEntry struct {
	Name string // 展示名
	Exe  string // 进程名（小写）
}

var (
	hwndSettings uintptr
	listItems    []progEntry // 与 ListView 行一一对应
)

// showSettingsWindow 打开（或激活）设置窗口
func showSettingsWindow() {
	if hwndSettings != 0 {
		if r, _, _ := procIsWindow.Call(hwndSettings); r != 0 {
			procSetForegroundWindow.Call(hwndSettings)
			return
		}
	}

	icc := INITCOMMONCONTROLSEX{DwSize: 8, DwICC: ICC_LISTVIEW_CLASSES | ICC_HOTKEY_CLASS}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("fastKeySettingsWnd")

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(settingsWndProc)
	wc.HInstance = hInstance
	wc.LpszClassName = className
	wc.HbrBackground = 16 // COLOR_BTNFACE + 1
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := syscall.UTF16PtrFromString("fastKey 设置")
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		WS_CAPTION|WS_SYSMENU|WS_VISIBLE,
		200, 150, 522, 570,
		0, 0, hInstance, 0)
	if hwnd == 0 {
		msgbox(0, "fastKey", "创建设置窗口失败", 0x30)
		return
	}
	hwndSettings = hwnd
	procUpdateWindow.Call(hwnd)
}

// createCtrl 创建子控件并设置默认 GUI 字体
func createCtrl(parent uintptr, class, text string, style uint32,
	x, y, w, h int32, id uintptr) uintptr {

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString(class)
	txt, _ := syscall.UTF16PtrFromString(text)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(txt)),
		uintptr(style),
		uintptr(int64(x)), uintptr(int64(y)), uintptr(int64(w)), uintptr(int64(h)),
		parent, id, hInstance, 0)
	if hwnd != 0 {
		font, _, _ := procGetStockObject.Call(DEFAULT_GUI_FONT)
		procSendMessageW.Call(hwnd, WM_SETFONT, font, 1)
	}
	return hwnd
}

func getDlgItem(parent uintptr, id uintptr) uintptr {
	h, _, _ := procGetDlgItem.Call(parent, id)
	return h
}

// populateList 填充程序列表（已安装 + 正在运行），按当前配置勾选
func populateList(hwnd uintptr) {
	list := getDlgItem(hwnd, IDC_LIST)
	if list == 0 {
		return
	}
	// 清空
	procSendMessageW.Call(list, 0x1009 /*LVM_DELETEALLITEMS*/, 0, 0)

	entries := collectPrograms()

	// 当前配置中的进程目标（小写）
	selected := map[string]bool{}
	for _, t := range currentCfg.Targets {
		if t.Process != "" {
			selected[strings.ToLower(t.Process)] = true
		}
	}
	// 配置里有的进程目标但不在枚举结果中（未运行/非注册表安装）：
	// 追加到列表中并保持勾选，避免保存时被静默丢弃
	inList := map[string]bool{}
	for _, e := range entries {
		inList[e.Exe] = true
	}
	for exe := range selected {
		if !inList[exe] {
			entries = append(entries, progEntry{Name: exe + "（来自当前配置）", Exe: exe})
		}
	}

	listItems = entries
	for i, e := range entries {
		text, _ := syscall.UTF16PtrFromString(fmt.Sprintf("%s    (%s)", e.Name, e.Exe))
		var item LVITEMW
		item.Mask = LVIF_TEXT
		item.IItem = int32(i)
		item.PszText = text
		procSendMessageW.Call(list, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))

		checked := uint32(1) // state image 1 = 未勾选
		if selected[e.Exe] {
			checked = 2 // 勾选
		}
		var st LVITEMW
		st.Mask = LVIF_STATE
		st.StateMask = LVIS_STATEIMAGEMASK
		st.State = checked << 12
		procSendMessageW.Call(list, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&st)))
	}
}

// collectPrograms 汇总已安装程序与正在运行的程序，去重排序
func collectPrograms() []progEntry {
	merged := map[string]progEntry{}
	for _, e := range enumInstalledPrograms() {
		if _, ok := merged[e.Exe]; !ok {
			merged[e.Exe] = e
		}
	}
	for _, e := range enumRunningPrograms() {
		if old, ok := merged[e.Exe]; !ok {
			merged[e.Exe] = e
		} else if old.Name == old.Exe {
			merged[e.Exe] = e // 用运行中窗口的更好名字替换
		}
	}
	out := make([]progEntry, 0, len(merged))
	for _, e := range merged {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Exe < out[j].Exe })
	return out
}

// addManualEntry 把手动输入的进程名加入列表并勾选
func addManualEntry(hwnd uintptr) {
	edit := getDlgItem(hwnd, IDC_EDIT_MANUAL)
	buf := make([]uint16, 260)
	n, _, _ := procGetWindowTextW.Call(edit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	exe := strings.TrimSpace(syscall.UTF16ToString(buf[:n]))
	if exe == "" {
		return
	}
	lower := strings.ToLower(exe)
	for _, e := range listItems {
		if e.Exe == lower {
			msgbox(hwnd, "fastKey", "该进程已在列表中，直接勾选即可。", 0x40)
			return
		}
	}
	list := getDlgItem(hwnd, IDC_LIST)
	i := len(listItems)
	listItems = append(listItems, progEntry{Name: exe, Exe: lower})
	text, _ := syscall.UTF16PtrFromString(fmt.Sprintf("%s    (%s)", exe, lower))
	var item LVITEMW
	item.Mask = LVIF_TEXT
	item.IItem = int32(i)
	item.PszText = text
	procSendMessageW.Call(list, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))
	var st LVITEMW
	st.Mask = LVIF_STATE
	st.StateMask = LVIS_STATEIMAGEMASK
	st.State = 2 << 12
	procSendMessageW.Call(list, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&st)))
	// 清空输入框
	empty, _ := syscall.UTF16PtrFromString("")
	procSendMessageW.Call(edit, 0x000C /*WM_SETTEXT*/, 0, uintptr(unsafe.Pointer(empty)))
}

// ---------------- 热键控件转换 ----------------

// hotkeyToCtrl 转为 msctls_hotkey32 的 HKM_SETHOTKEY 参数
func hotkeyToCtrl(mods, vk uint32) uintptr {
	var hkf uint32
	if mods&MOD_SHIFT != 0 {
		hkf |= HOTKEYF_SHIFT
	}
	if mods&MOD_CONTROL != 0 {
		hkf |= HOTKEYF_CONTROL
	}
	if mods&MOD_ALT != 0 {
		hkf |= HOTKEYF_ALT
	}
	return uintptr(vk | (hkf << 8))
}

// ctrlToHotkey 从 HKM_GETHOTKEY 结果还原（不支持 Win 键，返回 vk=0 表示空）
func ctrlToHotkey(w uintptr) (mods, vk uint32) {
	vk = uint32(w & 0xFF)
	hkf := uint32((w >> 8) & 0xFF)
	if hkf&HOTKEYF_SHIFT != 0 {
		mods |= MOD_SHIFT
	}
	if hkf&HOTKEYF_CONTROL != 0 {
		mods |= MOD_CONTROL
	}
	if hkf&HOTKEYF_ALT != 0 {
		mods |= MOD_ALT
	}
	return mods, vk
}

func setHotkeyCtrlValue(hwnd, id uintptr, hotkeyStr string) {
	ctrl := getDlgItem(hwnd, id)
	if ctrl == 0 || hotkeyStr == "" {
		return
	}
	mods, vk, err := parseHotkey(hotkeyStr)
	if err != nil {
		return
	}
	procSendMessageW.Call(ctrl, HKM_SETHOTKEY, hotkeyToCtrl(mods, vk), 0)
}

func getHotkeyCtrlValue(hwnd, id uintptr) string {
	ctrl := getDlgItem(hwnd, id)
	if ctrl == 0 {
		return ""
	}
	w, _, _ := procSendMessageW.Call(ctrl, HKM_GETHOTKEY, 0, 0)
	mods, vk := ctrlToHotkey(w)
	if vk == 0 {
		return ""
	}
	return formatHotkey(mods, vk)
}

// ---------------- 保存 ----------------

func saveSettings(hwnd uintptr) {
	mainHotkey := getHotkeyCtrlValue(hwnd, IDC_HOTKEY_MAIN)
	if mainHotkey == "" {
		msgbox(hwnd, "fastKey", "请设置“隐藏/恢复快捷键”。", 0x30)
		return
	}
	exitHotkey := getHotkeyCtrlValue(hwnd, IDC_HOTKEY_EXIT)

	// 收集勾选的进程目标
	list := getDlgItem(hwnd, IDC_LIST)
	count, _, _ := procSendMessageW.Call(list, LVM_GETITEMCOUNT, 0, 0)
	var targets []Target
	seen := map[string]bool{}
	for i := 0; i < int(count); i++ {
		state, _, _ := procSendMessageW.Call(list, LVM_GETITEMSTATE, uintptr(i), LVIS_STATEIMAGEMASK)
		if (state>>12)&0xF == 2 && i < len(listItems) {
			exe := listItems[i].Exe
			if !seen[exe] {
				targets = append(targets, Target{Process: exe})
				seen[exe] = true
			}
		}
	}
	// 保留配置里按标题匹配的目标（列表中不体现）
	for _, t := range currentCfg.Targets {
		if t.Process == "" && t.TitleContains != "" {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		msgbox(hwnd, "fastKey", "请至少勾选一个目标程序。", 0x30)
		return
	}

	newCfg := &Config{Hotkey: mainHotkey, ExitHotkey: exitHotkey, Targets: targets}
	if err := saveConfig(newCfg); err != nil {
		msgbox(hwnd, "fastKey", "保存配置文件失败: "+err.Error(), 0x30)
		return
	}
	currentCfg = newCfg
	if err := applyHotkeys(newCfg); err != nil {
		msgbox(hwnd, "fastKey", "配置已保存，但热键注册失败：\n"+err.Error(), 0x30)
		return
	}
	// 开机自启动开关（管理员自启优先，与普通自启互斥）
	autoStartMsg := ""
	adminChecked, normalChecked := false, false
	if chk := getDlgItem(hwnd, IDC_CHK_AUTOSTART_ADMIN); chk != 0 {
		r, _, _ := procSendMessageW.Call(chk, BM_GETCHECK, 0, 0)
		adminChecked = r == BST_CHECKED
	}
	if chk := getDlgItem(hwnd, IDC_CHK_AUTOSTART); chk != 0 {
		r, _, _ := procSendMessageW.Call(chk, BM_GETCHECK, 0, 0)
		normalChecked = r == BST_CHECKED
	}
	if adminChecked != isAdminAutostartEnabled() {
		if err := ensureAdminAutostart(adminChecked); err != nil {
			autoStartMsg += "\n（管理员自启动设置失败：" + err.Error() + "）"
		}
	}
	// 管理员自启生效时移除普通自启；否则按普通复选框设置
	if isAdminAutostartEnabled() {
		setAutostart(false)
	} else if normalChecked != isAutostartEnabled() {
		if err := setAutostart(normalChecked); err != nil {
			autoStartMsg += "\n（开机自启动设置失败：" + err.Error() + "）"
		}
	}
	// 刷新托盘提示
	if hwndMain != 0 {
		removeTrayIcon(hwndMain)
		addTrayIcon(hwndMain)
	}
	logf("配置已更新：热键 %s，目标 %d 个", newCfg.Hotkey, len(newCfg.Targets))
	msgbox(hwnd, "fastKey", "已保存并生效。"+autoStartMsg, 0x40)
	procDestroyWindow.Call(hwnd)
}

// ---------------- 设置窗口过程 ----------------

func settingsWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case 0x0001: // WM_CREATE
		createCtrl(hwnd, "Static", "目标程序（勾选后按快捷键隐藏）:", WS_CHILD|WS_VISIBLE,
			12, 8, 480, 18, 0)
		list := createCtrl(hwnd, "SysListView32", "",
			WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP|LVS_REPORT|LVS_SHOWSELALWAYS,
			12, 28, 486, 240, IDC_LIST)
		if list != 0 {
			procSendMessageW.Call(list, LVM_SETEXTENDEDLISTVIEWSTYLE,
				0, LVS_EX_CHECKBOXES|LVS_EX_FULLROWSELECT)
			colText, _ := syscall.UTF16PtrFromString("程序名称（进程名）")
			var col LVCOLUMNW
			col.Mask = LVCF_WIDTH | LVCF_TEXT | LVCF_SUBITEM
			col.Cx = 460
			col.PszText = colText
			procSendMessageW.Call(list, LVM_INSERTCOLUMNW, 0, uintptr(unsafe.Pointer(&col)))
		}

		createCtrl(hwnd, "Static", "手动添加进程名（如 WeChat.exe）:", WS_CHILD|WS_VISIBLE,
			12, 278, 480, 18, 0)
		createCtrl(hwnd, "Edit", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP|ES_AUTOHSCROLL,
			12, 298, 380, 24, IDC_EDIT_MANUAL)
		createCtrl(hwnd, "Button", "添加并勾选", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
			400, 298, 98, 24, IDC_BTN_ADD)
		createCtrl(hwnd, "Button", "刷新程序列表", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
			12, 332, 110, 26, IDC_BTN_REFRESH)

		createCtrl(hwnd, "Static", "隐藏/恢复快捷键:", WS_CHILD|WS_VISIBLE,
			12, 374, 140, 20, 0)
		createCtrl(hwnd, "msctls_hotkey32", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP,
			160, 372, 170, 24, IDC_HOTKEY_MAIN)
		createCtrl(hwnd, "Static", "退出快捷键:", WS_CHILD|WS_VISIBLE,
			12, 408, 140, 20, 0)
		createCtrl(hwnd, "msctls_hotkey32", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP,
			160, 406, 170, 24, IDC_HOTKEY_EXIT)
		createCtrl(hwnd, "Static", "提示：在快捷键框中直接按下组合键；按 Backspace 可清除。",
			WS_CHILD|WS_VISIBLE, 12, 440, 486, 18, 0)

		chk := createCtrl(hwnd, "Button", "开机自动启动（普通权限）",
			WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX,
			12, 470, 250, 22, IDC_CHK_AUTOSTART)
		if chk != 0 && isAutostartEnabled() {
			procSendMessageW.Call(chk, BM_SETCHECK, BST_CHECKED, 0)
		}
		chkAdmin := createCtrl(hwnd, "Button", "开机自动启动（管理员权限，可隐藏管理员程序）",
			WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX,
			12, 496, 268, 22, IDC_CHK_AUTOSTART_ADMIN)
		if chkAdmin != 0 && isAdminAutostartEnabled() {
			procSendMessageW.Call(chkAdmin, BM_SETCHECK, BST_CHECKED, 0)
		}

		createCtrl(hwnd, "Button", "保存", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
			290, 478, 96, 30, IDC_BTN_SAVE)
		createCtrl(hwnd, "Button", "取消", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
			396, 478, 96, 30, IDC_BTN_CANCEL)

		setHotkeyCtrlValue(hwnd, IDC_HOTKEY_MAIN, currentCfg.Hotkey)
		setHotkeyCtrlValue(hwnd, IDC_HOTKEY_EXIT, currentCfg.ExitHotkey)
		populateList(hwnd)
		return 0

	case WM_COMMAND:
		switch wParam & 0xFFFF {
		case IDC_BTN_ADD:
			addManualEntry(hwnd)
		case IDC_BTN_REFRESH:
			populateList(hwnd)
		case IDC_BTN_SAVE:
			saveSettings(hwnd)
		case IDC_BTN_CANCEL:
			procDestroyWindow.Call(hwnd)
		}
		return 0

	case WM_CLOSE:
		procDestroyWindow.Call(hwnd)
		return 0

	case WM_DESTROY:
		if hwnd == hwndSettings {
			hwndSettings = 0
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// ensureAdminAutostart 设置管理员自启动：已提权则直接操作；
// 普通权限下通过 UAC 启动提权辅助进程完成，并验证结果
func ensureAdminAutostart(enable bool) error {
	if isElevated() {
		return setAdminAutostart(enable)
	}
	arg := "-admin-autostart-off"
	if enable {
		arg = "-admin-autostart-on"
	}
	if !runElevatedHelper(arg) {
		return fmt.Errorf("提权操作被取消或失败")
	}
	time.Sleep(500 * time.Millisecond)
	if isAdminAutostartEnabled() != enable {
		return fmt.Errorf("设置未生效，请重试")
	}
	return nil
}
