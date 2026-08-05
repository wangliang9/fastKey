# -*- coding: utf-8 -*-
# 端到端测试：从 config.json 读取热键并模拟真实按键，验证 fastKey.exe 热键模式
import ctypes, json, subprocess, time, sys, os

os.chdir(os.path.dirname(os.path.abspath(__file__)))
user32 = ctypes.windll.user32
KEYEVENTF_KEYUP = 2

MOD_VK = {"ctrl": 0x11, "control": 0x11, "alt": 0x12, "shift": 0x10, "win": 0x5B}

def hotkey_to_vks(s):
    vks = []
    for part in s.split("+"):
        p = part.strip().lower()
        if p in MOD_VK:
            vks.append(MOD_VK[p])
        elif len(p) == 1 and p.isalpha():
            vks.append(ord(p.upper()))
        elif p.isdigit():
            vks.append(ord(p))
        elif p.startswith("f") and p[1:].isdigit():
            vks.append(0x70 + int(p[1:]) - 1)
        else:
            raise ValueError("unknown key: " + p)
    return vks

def send_hotkey(vks):
    for vk in vks:
        user32.keybd_event(vk, 0, 0, 0)
        time.sleep(0.05)
    for vk in reversed(vks):
        user32.keybd_event(vk, 0, KEYEVENTF_KEYUP, 0)
        time.sleep(0.05)

def notepad_pids():
    out = subprocess.check_output(
        ["tasklist", "/FI", "IMAGENAME eq notepad.exe", "/FO", "CSV", "/NH"]
    ).decode("gbk", errors="ignore")
    return [int(l.split('","')[1].strip('"')) for l in out.strip().splitlines()
            if "notepad" in l.lower()]

def notepad_window_states():
    pids = notepad_pids()
    states = []
    WndProc = ctypes.WINFUNCTYPE(ctypes.c_bool, ctypes.c_void_p, ctypes.c_void_p)
    def cb(hwnd, lparam):
        pid = ctypes.c_ulong(0)
        user32.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
        if pid.value in pids:
            states.append(bool(user32.IsWindowVisible(hwnd)))
        return True
    user32.EnumWindows(WndProc(cb), 0)
    return states

def fastkey_running():
    out = subprocess.check_output(
        ["tasklist", "/FI", "IMAGENAME eq fastKey.exe", "/FO", "CSV", "/NH"]
    ).decode("gbk", errors="ignore")
    return "fastKey.exe" in out

cfg = json.load(open("config.json", encoding="utf-8"))
toggle_vks = hotkey_to_vks(cfg["hotkey"])
exit_vks = hotkey_to_vks(cfg["exitHotkey"])
print("热键:", cfg["hotkey"], "→ VK:", [hex(v) for v in toggle_vks])

print("1) fastKey.exe 运行中:", fastkey_running())
s0 = notepad_window_states()
print("2) 初始记事本窗口可见状态:", s0)

send_hotkey(toggle_vks)
time.sleep(1.5)
s1 = notepad_window_states()
print("3) 按热键后可见状态:", s1)

send_hotkey(toggle_vks)
time.sleep(1.5)
s2 = notepad_window_states()
print("4) 再按热键后可见状态:", s2)

send_hotkey(exit_vks)
time.sleep(1.5)
print("5) 退出热键后 fastKey.exe 仍在运行:", fastkey_running())

ok = (any(s0) and s1 and not any(s1) and any(s2) and not fastkey_running())
print("端到端热键测试:", "通过" if ok else "失败")
sys.exit(0 if ok else 1)
