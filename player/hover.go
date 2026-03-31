package player

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"tilitili/source"
)

// macOS mouse-position helper. Compiled once to ~/.tilitili/bin/mousepos.
const mousePosHelperSwift = `import Cocoa
let loc = NSEvent.mouseLocation
let screen = NSScreen.main!.frame
print("\(Int(loc.x)),\(Int(screen.height - loc.y))")
`

// hoverWatcher monitors mouse position and toggles mpv visibility.
// When mouse is in the PiP corner area → restore mpv (unminimize, brighten, ontop).
// When mouse leaves → hide mpv (dim, minimize).
type hoverWatcher struct {
	mu        sync.Mutex
	ipcSocket string
	ipcConn   net.Conn
	cornerX   int // left edge of PiP area
	cornerY   int // top edge of PiP area
	cornerW   int // width
	cornerH   int // height
	stopCh    chan struct{}
	mouseCmd  string
	visible   bool
}

func newHoverWatcher(ipcSocket string, mouseCmd string) *hoverWatcher {
	screenW, screenH := getScreenSize()
	w := screenW / 4
	h := screenH / 4

	return &hoverWatcher{
		ipcSocket: ipcSocket,
		cornerX:   screenW - w - 60,
		cornerY:   screenH - h - 80,
		cornerW:   w + 80,
		cornerH:   h + 100,
		stopCh:    make(chan struct{}),
		mouseCmd:  mouseCmd,
		visible:   true, // starts visible, will hide after delay
	}
}

func (hw *hoverWatcher) start() {
	go func() {
		// Wait for mpv window to appear and IPC to be ready
		time.Sleep(2 * time.Second)
		hw.connectIPC()
		hw.hide()

		ticker := time.NewTicker(66 * time.Millisecond) // ~15Hz
		defer ticker.Stop()

		for {
			select {
			case <-hw.stopCh:
				hw.show() // restore before exit
				hw.disconnectIPC()
				return
			case <-ticker.C:
				x, y, err := hw.getMousePos()
				if err != nil {
					continue
				}
				inArea := x >= hw.cornerX && y >= hw.cornerY &&
					x <= hw.cornerX+hw.cornerW && y <= hw.cornerY+hw.cornerH

				hw.mu.Lock()
				wasVisible := hw.visible
				hw.mu.Unlock()

				if inArea && !wasVisible {
					hw.show()
				} else if !inArea && wasVisible {
					hw.hide()
				}
			}
		}
	}()
}

func (hw *hoverWatcher) stop() {
	select {
	case <-hw.stopCh:
	default:
		close(hw.stopCh)
	}
}

func (hw *hoverWatcher) show() {
	hw.mu.Lock()
	hw.visible = true
	hw.mu.Unlock()

	if source.Verbose {
		log.Printf("[DEBUG] Hover: showing mpv window")
	}

	hw.ipcCommand("set_property", "window-minimized", false)
	hw.ipcCommand("set_property", "brightness", 0)
	hw.ipcCommand("set_property", "contrast", 0)
	hw.ipcCommand("set_property", "saturation", 0)
	hw.ipcCommand("set_property", "ontop", "yes")
}

func (hw *hoverWatcher) hide() {
	hw.mu.Lock()
	hw.visible = false
	hw.mu.Unlock()

	if source.Verbose {
		log.Printf("[DEBUG] Hover: hiding mpv window")
	}

	hw.ipcCommand("set_property", "brightness", -100)
	hw.ipcCommand("set_property", "contrast", -100)
	hw.ipcCommand("set_property", "saturation", -100)
	hw.ipcCommand("set_property", "ontop", "no")
	hw.ipcCommand("set_property", "window-minimized", true)
}

func (hw *hoverWatcher) connectIPC() {
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("unix", hw.ipcSocket)
		if err == nil {
			hw.ipcConn = conn
			if source.Verbose {
				log.Printf("[DEBUG] Connected to mpv IPC socket")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	if source.Verbose {
		log.Printf("[DEBUG] Failed to connect to mpv IPC socket")
	}
}

func (hw *hoverWatcher) disconnectIPC() {
	if hw.ipcConn != nil {
		hw.ipcConn.Close()
	}
}

func (hw *hoverWatcher) ipcCommand(args ...any) {
	if hw.ipcConn == nil {
		return
	}
	msg := map[string]any{"command": args}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	hw.ipcConn.Write(data)
}

func (hw *hoverWatcher) getMousePos() (int, int, error) {
	out, err := exec.Command(hw.mouseCmd).Output()
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad mousepos output")
	}
	x, _ := strconv.Atoi(parts[0])
	y, _ := strconv.Atoi(parts[1])
	return x, y, nil
}

// --- Setup ---

// setupHover prepares the mouse helper and IPC socket path.
// Returns extra mpv args and the watcher (call watcher.start() after mpv starts).
func setupHover() (mpvArgs []string, watcher *hoverWatcher, err error) {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".tilitili")

	mouseCmd, err := ensureMousePosHelper(baseDir)
	if err != nil {
		return nil, nil, fmt.Errorf("mousepos helper: %w", err)
	}

	ipcSocket := filepath.Join(baseDir, "mpv-ipc")
	os.Remove(ipcSocket)

	mpvArgs = []string{
		fmt.Sprintf("--input-ipc-server=%s", ipcSocket),
	}

	watcher = newHoverWatcher(ipcSocket, mouseCmd)
	return mpvArgs, watcher, nil
}

// --- Mouse position helpers ---

func ensureMousePosHelper(baseDir string) (string, error) {
	binDir := filepath.Join(baseDir, "bin")
	os.MkdirAll(binDir, 0755)

	switch runtime.GOOS {
	case "darwin":
		return ensureMousePosDarwin(binDir)
	case "linux":
		return ensureMousePosLinux(binDir)
	default:
		return "", fmt.Errorf("hover not supported on %s", runtime.GOOS)
	}
}

func ensureMousePosDarwin(binDir string) (string, error) {
	helperPath := filepath.Join(binDir, "mousepos")
	if _, err := os.Stat(helperPath); err == nil {
		return helperPath, nil
	}

	if source.Verbose {
		log.Printf("[DEBUG] Compiling mousepos helper...")
	}

	srcPath := filepath.Join(binDir, "mousepos.swift")
	if err := os.WriteFile(srcPath, []byte(mousePosHelperSwift), 0644); err != nil {
		return "", err
	}
	defer os.Remove(srcPath)

	cmd := exec.Command("swiftc", "-O", "-o", helperPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compile: %s: %w", string(out), err)
	}
	return helperPath, nil
}

func ensureMousePosLinux(binDir string) (string, error) {
	helperPath := filepath.Join(binDir, "mousepos")
	if _, err := os.Stat(helperPath); err == nil {
		return helperPath, nil
	}

	script := `#!/bin/sh
eval $(xdotool getmouselocation --shell 2>/dev/null)
echo "${X},${Y}"
`
	if err := os.WriteFile(helperPath, []byte(script), 0755); err != nil {
		return "", err
	}
	return helperPath, nil
}

func getScreenSize() (int, int) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("swift", "-e", `
import Cocoa
let s = NSScreen.main!.frame
print("\(Int(s.width)),\(Int(s.height))")
`).Output()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(out)), ",")
			if len(parts) == 2 {
				w, _ := strconv.Atoi(parts[0])
				h, _ := strconv.Atoi(parts[1])
				return w, h
			}
		}
	}
	return 1920, 1080
}
