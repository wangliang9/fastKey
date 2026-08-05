# -*- coding: utf-8 -*-
# 托盘菜单“隐藏当前活动窗口”端到端测试
import ctypes, ctypes.wintypes as wt, json, subprocess, time, sys, os

os.chdir(os.path.dirname(os.path.abspath(__file__)))
user32 = ctypes.windll.user32
WM_COMMAND = 0x0111
ID_TRAY_HIDE_FG = 2003
KEYEVENTF_KEYUP = 2

user32.FindWindowExW.argtypes = [wt.HWND, wt.HWND, wt.LPCWSTR, wt.LPCWSTR]
user32.FindWindowExW.restype = wt.HWND

def send_hotkey(vks):
    for vk in vks:
        user32.keybd_event(vk, 0, 0, 0); time.sleep(0.05)
    for vk in reversed(vks):
        user32.keybd_event(vk, 0, KEYEVENTF_KEYUP, 0); time.sleep(0.05)

def notepad_main_window():
    out = subprocess.check_output(
        ["tasklist", "/FI", "IMAGENAME eq notepad.exe", "/FO", "CSV", "/NH"]
    ).decode("gbk", errors="ignore")
    pids = [int(l.split('","')[1].strip('"')) for l in out.strip().splitlines()
            if "notepad" in l.lower()]
    result = []
    WndProc = ctypes.WINFUNCTYPE(ctypes.c_bool, wt.HWND, wt.LPARAM)
    def cb(hwnd, _):
        pid = ctypes.c_ulong(0)
        user32.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
        if pid.value in pids and user32.IsWindowVisible(hwnd):
            result.append(hwnd)
        return True
    user32.EnumWindows(WndProc(cb), 0)
    return result[0] if result else 0

def fastkey_main():
    return user32.FindWindowExW(None, None, "fastKeyMainWnd", None)

def fastkey_running():
    out = subprocess.check_output(
        ["tasklist", "/FI", "IMAGENAME eq fastKey.exe", "/FO", "CSV", "/NH"]
    ).decode("gbk", errors="ignore")
    return "fastKey.exe" in out

cfg = json.load(open("config.json", encoding="utf-8"))
toggle_vk = 0x70 + int(cfg["hotkey"][1:]) - 1  # F9 形式

# 1) 记事本置前
np = notepad_main_window()
print("1) 记事本前台窗口:", hex(np) if np else "未找到")
user32.SetForegroundWindow(np)
time.sleep(0.5)

# 2) 向 fastKey 发送“隐藏当前活动窗口”菜单命令
fk = fastkey_main()
print("2) fastKey 消息窗口:", hex(fk) if fk else "未找到")
user32.PostMessageW(fk, WM_COMMAND, ID_TRAY_HIDE_FG, 0)
time.sleep(1.5)

np_after_hide = notepad_main_window()
print("3) 隐藏命令后记事本可见窗口:", "已隐藏 ✓" if not np_after_hide else "仍可见 ✗")

# 3) 按热键恢复
send_hotkey([toggle_vk])
time.sleep(1.5)
np_restored = notepad_main_window()
print("4) 热键恢复后记事本可见窗口:", "已恢复 ✓" if np_restored else "未恢复 ✗")

# 4) 排除性验证：fastKey 的设置窗口等自身窗口不会被当作目标（直接再发一次命令，
#    此时前台是记事本，应再次隐藏它而非出错）
user32.SetForegroundWindow(np_restored or np)
time.sleep(0.5)
user32.PostMessageW(fk, WM_COMMAND, ID_TRAY_HIDE_FG, 0)
time.sleep(1.5)
np_second_hide = notepad_main_window()
print("5) 再次隐藏命令:", "已隐藏 ✓" if not np_second_hide else "仍可见 ✗")

# 5) 退出（退出前自动恢复所有隐藏窗口）
send_hotkey([0x11, 0x12, 0x51])  # Ctrl+Alt+Q
time.sleep(1.5)
running = fastkey_running()
np_final = notepad_main_window()
print("6) 退出后 fastKey 已停止:", not running, "；记事本被自动恢复:", bool(np_final))

ok = (np and fk and not np_after_hide and np_restored
      and not np_second_hide and not running and np_final)
print("临时隐藏活动窗口测试:", "通过" if ok else "失败")
sys.exit(0 if ok else 1)
