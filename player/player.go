package player

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"tilitili/bilibili"
	"tilitili/config"
)

// Player manages video/audio playback via mpv or browser fallback.
type Player struct {
	mu  sync.Mutex
	cmd *exec.Cmd // running mpv process, nil if nothing playing
	Cfg *config.Config
}

func New(cfg *config.Config) *Player {
	return &Player{Cfg: cfg}
}

func (p *Player) Play(url, title string, audioOnly, newTab bool) string {
	// -t flag: open in browser tab (no mpv)
	if newTab {
		return p.openInBrowser(url, title)
	}

	// Stop any existing playback first
	p.stopMpv()

	// Try mpv (best experience), fall back to browser
	if mpvPath, err := exec.LookPath("mpv"); err == nil {
		return p.playMpv(mpvPath, url, title, audioOnly)
	}

	// No mpv — open in browser as new window
	return p.openInBrowserWindow(url, title, audioOnly)
}

func (p *Player) playMpv(mpvPath, url, title string, audioOnly bool) string {
	args := []string{
		"--no-terminal",
		fmt.Sprintf("--title=%s", title),
	}

	// Use a platform-appropriate OSD/subtitle font
	font := osdFont()
	args = append(args, fmt.Sprintf("--osd-font=%s", font), fmt.Sprintf("--sub-font=%s", font))

	if audioOnly {
		args = append(args, "--no-video")
	} else {
		// PiP window sized and positioned from config
		geometry := p.Cfg.MpvGeometry()
		args = append(args, fmt.Sprintf("--geometry=%s", geometry))
		if p.Cfg.Ontop {
			args = append(args, "--ontop")
		}
	}

	args = append(args, url)

	if bilibili.Verbose {
		log.Printf("[DEBUG] Running: %s %v", mpvPath, args)
	}

	cmd := exec.Command(mpvPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("Failed to start mpv: %v", err)
	}

	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	// Wait for mpv to finish in background, then clear the reference
	go func() {
		cmd.Wait()
		p.mu.Lock()
		if p.cmd == cmd {
			p.cmd = nil
		}
		p.mu.Unlock()
	}()

	if audioOnly {
		return fmt.Sprintf("Playing audio: %s", title)
	}
	return fmt.Sprintf("Playing video: %s", title)
}

func (p *Player) Stop() string {
	if stopped := p.stopMpv(); stopped {
		return "Playback stopped."
	}
	return "Nothing is playing."
}

func (p *Player) stopMpv() bool {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
		p.mu.Lock()
		p.cmd = nil
		p.mu.Unlock()
		return true
	}
	return false
}

func (p *Player) Cleanup() {
	p.stopMpv()
}

// --- Platform helpers ---

// osdFont returns a font name that exists on the current platform.
func osdFont() string {
	switch runtime.GOOS {
	case "darwin":
		return "Helvetica Neue"
	case "windows":
		return "Segoe UI"
	default: // linux, freebsd, etc.
		return "sans-serif"
	}
}

// openURL opens a URL in the platform's default browser.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// --- Browser fallback ---

func (p *Player) openInBrowser(url, title string) string {
	switch runtime.GOOS {
	case "darwin":
		return p.openTabDarwin(url, title)
	default:
		// Linux/Windows: just open URL (browser decides tab vs window)
		if err := openURL(url); err != nil {
			return fmt.Sprintf("Failed to open browser: %v", err)
		}
		return fmt.Sprintf("Playing video (browser): %s", title)
	}
}

func (p *Player) openInBrowserWindow(url, title string, audioOnly bool) string {
	switch runtime.GOOS {
	case "darwin":
		return p.openWindowDarwin(url, title, audioOnly)
	default:
		// Linux/Windows: try chrome --new-window, fall back to default browser
		if chrome := findChromeLinuxWindows(); chrome != "" {
			cmd := exec.Command(chrome, "--new-window", url)
			if err := cmd.Start(); err == nil {
				if audioOnly {
					return fmt.Sprintf("Playing audio (browser): %s", title)
				}
				return fmt.Sprintf("Playing video (browser): %s", title)
			}
		}
		if err := openURL(url); err != nil {
			return fmt.Sprintf("Failed to open browser: %v", err)
		}
		if audioOnly {
			return fmt.Sprintf("Playing audio (browser): %s", title)
		}
		return fmt.Sprintf("Opened in browser: %s", title)
	}
}

// --- macOS-specific browser functions ---

func (p *Player) openTabDarwin(url, title string) string {
	_, browserApp := defaultBrowserDarwin()
	if browserApp == "" {
		openURL(url)
		return fmt.Sprintf("Playing video (new tab): %s", title)
	}

	script := fmt.Sprintf(`tell application "%s"
	tell front window
		make new tab with properties {URL:"%s"}
	end tell
	activate
end tell`, browserApp, url)

	if bilibili.Verbose {
		log.Printf("[DEBUG] Opening new tab in %s via AppleScript", browserApp)
	}

	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		if bilibili.Verbose {
			log.Printf("[DEBUG] AppleScript tab failed: %s, falling back to open", string(out))
		}
		openURL(url)
	}
	return fmt.Sprintf("Playing video (new tab): %s", title)
}

func (p *Player) openWindowDarwin(url, title string, audioOnly bool) string {
	_, browserApp := defaultBrowserDarwin()
	if browserApp == "" {
		openURL(url)
		return fmt.Sprintf("Opened in browser: %s", title)
	}

	script := fmt.Sprintf(`tell application "%s"
	make new window
	tell front window
		make new tab with properties {URL:"%s"}
	end tell
	activate
end tell`, browserApp, url)

	if bilibili.Verbose {
		log.Printf("[DEBUG] Opening new window in %s via AppleScript", browserApp)
	}

	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		if bilibili.Verbose {
			log.Printf("[DEBUG] AppleScript window failed: %s, falling back to open", string(out))
		}
		openURL(url)
	}

	if audioOnly {
		return fmt.Sprintf("Playing audio (browser): %s", title)
	}
	return fmt.Sprintf("Playing video (browser): %s", title)
}

// defaultBrowserDarwin returns the app path and process name of the default browser.
func defaultBrowserDarwin() (appPath, processName string) {
	out, err := exec.Command("swift", "-e", `
import AppKit
if let url = NSWorkspace.shared.urlForApplication(toOpen: URL(string: "https://example.com")!) {
    print(url.path)
}`).Output()
	if err != nil {
		return "", ""
	}
	appPath = strings.TrimSpace(string(out))
	if appPath == "" {
		return "", ""
	}

	nameOut, err := exec.Command("defaults", "read",
		appPath+"/Contents/Info.plist", "CFBundleName").Output()
	if err == nil {
		processName = strings.TrimSpace(string(nameOut))
	} else {
		base := appPath
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		processName = strings.TrimSuffix(base, ".app")
	}
	return appPath, processName
}

// --- Linux/Windows Chrome detection ---

func findChromeLinuxWindows() string {
	if runtime.GOOS == "windows" {
		// Common Chrome paths on Windows
		paths := []string{
			os.Getenv("PROGRAMFILES") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES(X86)") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("LOCALAPPDATA") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("PROGRAMFILES") + `\Microsoft\Edge\Application\msedge.exe`,
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
	// Linux
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium-browser", "chromium", "microsoft-edge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
