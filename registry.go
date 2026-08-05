// registry.go - 枚举已安装程序（注册表 Uninstall 键）与正在运行的 GUI 程序
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegEnumKeyExW    = advapi32.NewProc("RegEnumKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW        = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW       = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey           = advapi32.NewProc("RegCloseKey")
	procOpenProcessToken      = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation   = advapi32.NewProc("GetTokenInformation")
)

const (
	HKEY_LOCAL_MACHINE = 0x80000002
	HKEY_CURRENT_USER  = 0x80000001

	KEY_READ        = 0x20019
	KEY_WOW64_32KEY = 0x0200
	KEY_WOW64_64KEY = 0x0100

	REG_SZ        = 1
	REG_EXPAND_SZ = 2
	REG_DWORD     = 4

	uninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	runPath       = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	autostartName = "fastKey"
)

// ---------------- 开机自启动（HKCU Run 键） ----------------
// isAutostartEnabled 查询是否已设置开机自启动
func isAutostartEnabled() bool {
	h, ok := regOpen(HKEY_CURRENT_USER, runPath, KEY_READ)
	if !ok {
		return false
	}
	defer procRegCloseKey.Call(h)
	return regQueryString(h, autostartName) != ""
}

// setAutostart 设置或取消开机自启动（写入/删除 Run 键中的 exe 路径）
func setAutostart(enable bool) error {
	h, ok := regOpen(HKEY_CURRENT_USER, runPath, KEY_READ|0x0002 /*KEY_SET_VALUE*/)
	if !ok {
		return fmt.Errorf("打开注册表 Run 键失败")
	}
	defer procRegCloseKey.Call(h)

	name, _ := syscall.UTF16PtrFromString(autostartName)
	if !enable {
		// 值不存在时删除会报错，忽略
		procRegDeleteValueW.Call(h, uintptr(unsafe.Pointer(name)))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	u16 := syscall.StringToUTF16(`"` + exe + `"`)
	data := make([]byte, len(u16)*2)
	for i, c := range u16 {
		data[i*2] = byte(c)
		data[i*2+1] = byte(c >> 8)
	}
	r, _, _ := procRegSetValueExW.Call(h, uintptr(unsafe.Pointer(name)), 0,
		REG_SZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	if r != 0 {
		return fmt.Errorf("写入注册表失败（错误码 %d）", r)
	}
	return nil
}

func regOpen(root uintptr, subkey string, sam uint32) (uintptr, bool) {
	sk, _ := syscall.UTF16PtrFromString(subkey)
	var h uintptr
	r, _, _ := procRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(sk)), 0, uintptr(sam),
		uintptr(unsafe.Pointer(&h)))
	return h, r == 0
}

func regQueryString(h uintptr, name string) string {
	n, _ := syscall.UTF16PtrFromString(name)
	var typ uint32
	var size uint32
	r, _, _ := procRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || size == 0 || (typ != REG_SZ && typ != REG_EXPAND_SZ) {
		return ""
	}
	buf := make([]byte, size)
	r, _, _ = procRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return ""
	}
	// UTF-16LE → string
	u16 := make([]uint16, size/2)
	for i := range u16 {
		u16[i] = uint16(buf[i*2]) | uint16(buf[i*2+1])<<8
	}
	return strings.TrimRight(syscall.UTF16ToString(u16), "\x00")
}

func regQueryDword(h uintptr, name string) uint32 {
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, size uint32 = REG_DWORD, 4
	var val uint32
	r, _, _ := procRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&val)),
		uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return 0
	}
	return val
}

// extractExePath 从 DisplayIcon / InstallLocation 等字段推断主程序 exe 路径
func extractExePath(displayIcon string) string {
	s := strings.Trim(displayIcon, `"`)
	// DisplayIcon 常为 "C:\...\app.exe,0" 形式
	lower := strings.ToLower(s)
	idx := strings.Index(lower, ".exe")
	if idx < 0 {
		return ""
	}
	p := s[:idx+4]
	// 去掉可能的前导引号/参数
	if i := strings.LastIndexAny(p, `"`); i >= 0 {
		p = p[i+1:]
	}
	if !filepath.IsAbs(p) {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// enumInstalledPrograms 从注册表 Uninstall 键枚举已安装程序
func enumInstalledPrograms() []progEntry {
	var out []progEntry
	roots := []struct {
		root uintptr
		sam  uint32
	}{
		{HKEY_LOCAL_MACHINE, KEY_READ | KEY_WOW64_64KEY},
		{HKEY_LOCAL_MACHINE, KEY_READ | KEY_WOW64_32KEY},
		{HKEY_CURRENT_USER, KEY_READ},
	}
	for _, rt := range roots {
		h, ok := regOpen(rt.root, uninstallPath, rt.sam)
		if !ok {
			continue
		}
		for i := 0; ; i++ {
			nameBuf := make([]uint16, 256)
			nameLen := uint32(len(nameBuf))
			r, _, _ := procRegEnumKeyExW.Call(h, uintptr(i),
				uintptr(unsafe.Pointer(&nameBuf[0])), uintptr(unsafe.Pointer(&nameLen)),
				0, 0, 0, 0)
			if r != 0 {
				break
			}
			subName := syscall.UTF16ToString(nameBuf[:nameLen])
			sh, ok := regOpen(rt.root, uninstallPath+`\`+subName, rt.sam)
			if !ok {
				continue
			}
			displayName := regQueryString(sh, "DisplayName")
			systemComponent := regQueryDword(sh, "SystemComponent")
			displayIcon := regQueryString(sh, "DisplayIcon")
			procRegCloseKey.Call(sh)

			if displayName == "" || systemComponent == 1 {
				continue
			}
			exePath := extractExePath(displayIcon)
			if exePath == "" {
				continue
			}
			out = append(out, progEntry{
				Name: displayName,
				Exe:  strings.ToLower(filepath.Base(exePath)),
			})
		}
		procRegCloseKey.Call(h)
	}
	return out
}

// enumRunningPrograms 枚举正在运行且拥有可见顶层窗口的程序
func enumRunningPrograms() []progEntry {
	procNames := snapshotProcesses()
	seen := map[string]bool{}
	var out []progEntry

	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		owner, _, _ := procGetWindow.Call(hwnd, GW_OWNER)
		if owner != 0 || !isWindowVisible(hwnd) {
			return 1
		}
		title := windowTitle(hwnd)
		if title == "" {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		exe := procNames[pid]
		if exe == "" || seen[exe] {
			return 1
		}
		seen[exe] = true
		// 展示名：优先用窗口标题（截断过长的）
		name := title
		if len([]rune(name)) > 40 {
			name = string([]rune(name)[:40]) + "…"
		}
		out = append(out, progEntry{Name: name + "（运行中）", Exe: exe})
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}

// ---------------- 管理员权限开机自启动（计划任务） ----------------
//
// Run 键无法启动需提权的程序，改用计划任务（/rl highest）：
// 登录时以管理员权限静默启动，无 UAC 提示。
// 注意：创建/删除最高权限任务本身需要管理员权限，普通权限下会失败，
// 调用方应通过 ensureAdminAutostart（gui.go）走提权辅助进程。

const adminTaskName = "fastKey AutoStart"

func runSchtasks(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isAdminAutostartEnabled 查询管理员自启动计划任务是否存在
func isAdminAutostartEnabled() bool {
	return runSchtasks("/query", "/tn", adminTaskName) == nil
}

// setAdminAutostart 创建/删除管理员自启动计划任务（需要管理员权限）
func setAdminAutostart(enable bool) error {
	if !enable {
		err := runSchtasks("/delete", "/tn", adminTaskName, "/f")
		// 任务不存在时删除报错，视为成功
		if err != nil && !isAdminAutostartEnabled() {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	return runSchtasks("/create", "/tn", adminTaskName,
		"/tr", `"`+exe+`"`, "/sc", "onlogon", "/rl", "highest", "/f")
}
