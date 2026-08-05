# -*- coding: utf-8 -*-
# 设置窗口自动化测试：
# 1. 启动 fastKey.exe -settings，验证设置窗口创建
# 2. 枚举子控件：ListView、两个热键控件、按钮
# 3. 验证程序列表非空
# 4. 点击“保存”，处理“已保存”提示框，验证窗口关闭、config.json 有效
import ctypes, ctypes.wintypes as wt, json, subprocess, time, sys, os

os.chdir(os.path.dirname(os.path.abspath(__file__)))
user32 = ctypes.windll.user32
BM_CLICK = 0x00F5
WM_COMMAND = 0x0111
LVM_GETITEMCOUNT = 0x1004

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

# 1) 窗口创建
win = wait_for(lambda: find_window("fastKey 设置", "fastKeySettingsWnd"))
print("1) 设置窗口句柄:", win, "OK" if win else "FAIL")
if not win:
    sys.exit(1)

# 2) 子控件
kids = enum_children(win)
classes = [c for _, c, _ in kids]
n_list = classes.count("SysListView32")
n_hotkey = classes.count("msctls_hotkey32")
n_button = classes.count("Button")
print(f"2) 子控件: ListView×{n_list} 热键框×{n_hotkey} 按钮×{n_button}",
      "OK" if (n_list == 1 and n_hotkey == 2 and n_button >= 4) else "FAIL")

# 3) 程序列表
list_hwnd = next(h for h, c, _ in kids if c == "SysListView32")
count = user32.SendMessageW(list_hwnd, LVM_GETITEMCOUNT, 0, 0)
print("3) 程序列表条目数:", count, "OK" if count > 0 else "FAIL")

# 4) 点保存 → 处理提示框 → 窗口应关闭
# 用 PostMessage 避免跨线程 SendMessage 与模态提示框互相等待
save_hwnd = next(h for h, c, t in kids if c == "Button" and t == "保存")
user32.PostMessageW(save_hwnd, BM_CLICK, 0, 0)
dlg = wait_for(lambda: find_window("fastKey", "#32770"), timeout=5)
if dlg:
    # 模态提示框：直接对其“确定”按钮 SendMessage BM_CLICK（此时主测试线程未阻塞，可安全调用）
    btn = [h for h, c, t in enum_children(dlg) if c == "Button"]
    if btn:
        user32.SendMessageW(btn[0], BM_CLICK, 0, 0)
    print("4) “已保存”提示框已确认")
time.sleep(1)
still = find_window("fastKey 设置", "fastKeySettingsWnd")
print("5) 保存后设置窗口已关闭:", "OK" if not still else "FAIL")

# 6) config.json 有效且 targets 非空
cfg = json.load(open("config.json", encoding="utf-8"))
valid = bool(cfg.get("hotkey")) and len(cfg.get("targets", [])) > 0
print("6) config.json 有效:", "OK" if valid else "FAIL",
      "| hotkey =", cfg.get("hotkey"), "| targets =", len(cfg.get("targets", [])))

ok = bool(win) and n_list == 1 and n_hotkey == 2 and count > 0 and not still and valid
print("设置窗口测试:", "通过" if ok else "失败")
sys.exit(0 if ok else 1)
