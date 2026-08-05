# -*- coding: utf-8 -*-
# 开机自启动开关端到端测试
import ctypes, ctypes.wintypes as wt, subprocess, time, sys, os

os.chdir(os.path.dirname(os.path.abspath(__file__)))
user32 = ctypes.windll.user32
BM_CLICK, BM_GETCHECK, BM_SETCHECK = 0x00F5, 0x00F0, 0x00F1
BST_CHECKED = 1

def reg_query():
    out = subprocess.run(
        ["reg", "query", r"HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Run",
         "/v", "fastKey"], capture_output=True, text=True)
    if out.returncode != 0:
        return None
    for line in out.stdout.splitlines():
        if "fastKey" in line and "REG_SZ" in line:
            return line.split("REG_SZ", 1)[1].strip()
    return None

def find_window(title_sub=None, class_name=None):
    found = []
    WndProc = ctypes.WINFUNCTYPE(ctypes.c_bool, wt.HWND, wt.LPARAM)
    def cb(hwnd, _):
        buf = ctypes.create_unicode_buffer(256)
        user32.GetWindowTextW(hwnd, buf, 256)
        cls = ctypes.create_unicode_buffer(64)
        user32.GetClassNameW(hwnd, cls, 64)
        if title_sub and title_sub in buf.value and (not class_name or cls.value == class_name):
            found.append(hwnd)
        return True
    user32.EnumWindows(WndProc(cb), 0)
    return found[0] if found else 0

def enum_children(hwnd):
    kids = []
    WndProc = ctypes.WINFUNCTYPE(ctypes.c_bool, wt.HWND, wt.LPARAM)
    def cb(h, _):
        cls = ctypes.create_unicode_buffer(64)
        user32.GetClassNameW(h, cls, 64)
        buf = ctypes.create_unicode_buffer(256)
        user32.GetWindowTextW(h, buf, 256)
        kids.append((h, cls.value, buf.value))
        return True
    user32.EnumChildWindows(hwnd, WndProc(cb), 0)
    return kids

def wait_for(fn, timeout=10):
    t0 = time.time()
    while time.time() - t0 < timeout:
        v = fn()
        if v:
            return v
        time.sleep(0.3)
    return 0

def open_settings():
    if fastkey_running():
        # 复用已运行实例：找到其消息窗口，模拟托盘“设置”菜单命令
        user32.FindWindowExW.argtypes = [wt.HWND, wt.HWND, wt.LPCWSTR, wt.LPCWSTR]
        user32.FindWindowExW.restype = wt.HWND
        hwnd = user32.FindWindowExW(None, None, "fastKeyMainWnd", None)
        if hwnd:
            user32.PostMessageW(hwnd, 0x0111, 2001, 0)  # WM_COMMAND, ID_TRAY_SETTINGS
    else:
        subprocess.Popen(["cmd", "/c", "start", "", r"D:\projects\fastKey\fastKey.exe", "-settings"],
                         shell=False)
    return wait_for(lambda: find_window("fastKey 设置", "fastKeySettingsWnd"))

def click_save_and_confirm(win):
    kids = enum_children(win)
    save = next(h for h, c, t in kids if c == "Button" and t == "保存")
    user32.PostMessageW(save, BM_CLICK, 0, 0)
    dlg = wait_for(lambda: find_window("fastKey", "#32770"), timeout=5)
    if dlg:
        btn = [h for h, c, t in enum_children(dlg) if c == "Button"]
        if btn:
            user32.SendMessageW(btn[0], BM_CLICK, 0, 0)
    wait_for(lambda: not find_window("fastKey 设置", "fastKeySettingsWnd"), timeout=5)

def fastkey_running():
    out = subprocess.check_output(
        ["tasklist", "/FI", "IMAGENAME eq fastKey.exe", "/FO", "CSV", "/NH"]
    ).decode("gbk", errors="ignore")
    return "fastKey.exe" in out

results = []
print("0) 初始注册表 Run\\fastKey:", reg_query())

# --- 第一轮：勾选并保存 ---
win = open_settings()
chk = next(h for h, c, t in enum_children(win)
           if c == "Button" and "开机自动启动" in t)
state0 = user32.SendMessageW(chk, BM_GETCHECK, 0, 0)
print("1) 打开设置，复选框初始状态:", "已勾选" if state0 else "未勾选")
results.append(state0 == 0)

user32.SendMessageW(chk, BM_SETCHECK, BST_CHECKED, 0)  # 勾选
click_save_and_confirm(win)
v1 = reg_query()
print("2) 勾选保存后注册表值:", v1)
results.append(v1 is not None and "fastKey.exe" in v1)

# --- 第二轮：回显 + 取消勾选 ---
time.sleep(1)
win = open_settings()
chk = next(h for h, c, t in enum_children(win)
           if c == "Button" and "开机自动启动" in t)
state1 = user32.SendMessageW(chk, BM_GETCHECK, 0, 0)
print("3) 重新打开设置，复选框回显:", "已勾选" if state1 else "未勾选")
results.append(state1 == BST_CHECKED)

user32.SendMessageW(chk, BM_SETCHECK, 0, 0)  # 取消勾选
click_save_and_confirm(win)
v2 = reg_query()
print("4) 取消勾选保存后注册表值:", v2)
results.append(v2 is None)

# 退出 fastKey
for vk in (0x11, 0x12, 0x51):
    user32.keybd_event(vk, 0, 0, 0); time.sleep(0.05)
for vk in (0x51, 0x12, 0x11):
    user32.keybd_event(vk, 0, 2, 0); time.sleep(0.05)
time.sleep(1.5)
running = fastkey_running()
print("5) fastKey 已退出:", not running)
results.append(not running)

ok = all(results)
print("开机自启动测试:", "通过" if ok else "失败", results)
sys.exit(0 if ok else 1)
