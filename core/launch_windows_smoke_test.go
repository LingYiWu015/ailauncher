//go:build windows

// Windows 集成冒烟测试（真实拉起进程，无需 TTY）：
//   - TestWmiSmoke：真实跑 writeWmiVBS + wscript + WMI Create 链路，
//     确认 WmiPrvSE 能拉起目标进程（写 marker），wscript 退出码为 0。
//     抓 vbs 语法这类「字符串单测覆盖不到」的问题（见 devlog 05）。
//   - TestLaunchGuiDetached：真实走 Launch(gui) 的 detached 启动链路。

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWmiSmoke(t *testing.T) {
	marker := filepath.Join(os.TempDir(), "ailauncher-wmi-smoke.txt")
	os.Remove(marker)

	cmdline := "cmd.exe /c echo WMI_OK > " + marker
	script, err := writeWmiVBS(cmdline, os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(script)
	defer os.Remove(marker)

	if err := exec.Command("wscript.exe", script).Run(); err != nil {
		t.Fatalf("wscript failed (exit != 0): %v", err)
	}
	// Create 返回时目标进程刚被创建、未必已写完 marker，轮询等待（最多 5s）。
	var b []byte
	for i := 0; i < 50; i++ {
		if b, err = os.ReadFile(marker); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("marker not written — WMI did not create the process: %v", err)
	}
	if !strings.Contains(string(b), "WMI_OK") {
		t.Fatalf("marker content = %q, want WMI_OK", string(b))
	}
	t.Logf("WMI smoke OK: %s", strings.TrimSpace(string(b)))
}

// TestLaunchGuiDetached 验证 gui 型 agent 的 detached 启动真能拉起进程
// （无害 cmd 写 marker；直拉或 WMI 兜底任一成功即可）。
func TestLaunchGuiDetached(t *testing.T) {
	marker := filepath.Join(os.TempDir(), "ailauncher-gui-smoke.txt")
	os.Remove(marker)
	defer os.Remove(marker)

	agent := &Agent{Name: "smoke", Exec: "cmd.exe", Type: "gui"}
	if err := Launch(agent, os.TempDir(), nil,
		[]string{"/c", "echo", "gui_ok", ">", marker}, "unused"); err != nil {
		t.Fatalf("Launch(gui) failed: %v", err)
	}
	var b []byte
	var err error
	for i := 0; i < 50; i++ {
		if b, err = os.ReadFile(marker); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("gui-launched process did not run (marker missing): %v", err)
	}
	if !strings.Contains(string(b), "gui_ok") {
		t.Fatalf("marker content = %q, want gui_ok", string(b))
	}
	t.Logf("gui detached smoke OK: %s", strings.TrimSpace(string(b)))
}
