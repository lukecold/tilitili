package player

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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
		"--osd-font=Helvetica Neue",
		"--sub-font=Helvetica Neue",
	}

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

// --- Browser fallback (used for -t tab mode and when mpv is not installed) ---

func (p *Player) openInBrowser(url, title string) string {
	_, browserApp := defaultBrowserDarwin()
	if browserApp == "" {
		exec.Command("open", url).Start()
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
		exec.Command("open", url).Start()
	}
	return fmt.Sprintf("Playing video (new tab): %s", title)
}

func (p *Player) openInBrowserWindow(url, title string, audioOnly bool) string {
	_, browserApp := defaultBrowserDarwin()
	if browserApp == "" {
		exec.Command("open", url).Start()
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
		exec.Command("open", url).Start()
	}

	if audioOnly {
		return fmt.Sprintf("Playing audio (browser): %s", title)
	}
	return fmt.Sprintf("Playing video (browser): %s", title)
}

// defaultBrowserDarwin returns the app path and process name of the default browser.
// Uses Swift to query NSWorkspace (the only fully reliable method on macOS).
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
