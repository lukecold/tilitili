// Package deps handles auto-downloading of runtime dependencies (mpv, yt-dlp).
// Binaries are stored in ~/.tilitili/bin/ and looked up from there if not on PATH.
package deps

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var binDir string

func init() {
	home, _ := os.UserHomeDir()
	binDir = filepath.Join(home, ".tilitili", "bin")
}

// BinDir returns the path where tilitili stores downloaded binaries.
func BinDir() string { return binDir }

// EnsureDeps checks for mpv and yt-dlp, offering to download them if missing.
// Returns paths to mpv and yt-dlp binaries.
func EnsureDeps() (mpvPath, ytdlpPath string, err error) {
	mpvPath = findBinary("mpv")
	ytdlpPath = findBinary("yt-dlp")

	if mpvPath != "" && ytdlpPath != "" {
		return mpvPath, ytdlpPath, nil
	}

	// Create bin dir if needed
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return mpvPath, ytdlpPath, fmt.Errorf("failed to create %s: %w", binDir, err)
	}

	if ytdlpPath == "" {
		fmt.Println("yt-dlp not found. Downloading...")
		ytdlpPath, err = downloadYtdlp()
		if err != nil {
			return mpvPath, ytdlpPath, fmt.Errorf("failed to download yt-dlp: %w", err)
		}
		fmt.Printf("yt-dlp installed to %s\n", ytdlpPath)
	}

	if mpvPath == "" {
		fmt.Println("mpv not found. Downloading...")
		mpvPath, err = downloadMpv()
		if err != nil {
			return mpvPath, ytdlpPath, fmt.Errorf("failed to download mpv: %w", err)
		}
		fmt.Printf("mpv installed to %s\n", mpvPath)
	}

	return mpvPath, ytdlpPath, nil
}

// findBinary checks PATH first, then ~/.tilitili/bin/.
func findBinary(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		name += ".exe"
	}
	// Check system PATH
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// Check our bin dir
	p := filepath.Join(binDir, name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// --- yt-dlp download ---

func downloadYtdlp() (string, error) {
	var asset string
	switch runtime.GOOS {
	case "darwin":
		asset = "yt-dlp_macos"
	case "linux":
		if runtime.GOARCH == "arm64" {
			asset = "yt-dlp_linux_aarch64"
		} else {
			asset = "yt-dlp_linux"
		}
	case "windows":
		asset = "yt-dlp.exe"
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	url := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/" + asset

	destName := "yt-dlp"
	if runtime.GOOS == "windows" {
		destName = "yt-dlp.exe"
	}
	dest := filepath.Join(binDir, destName)

	if err := downloadFile(url, dest); err != nil {
		return "", err
	}

	if runtime.GOOS != "windows" {
		os.Chmod(dest, 0755)
	}
	return dest, nil
}

// --- mpv download ---

func downloadMpv() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return downloadMpvDarwin()
	case "linux":
		return downloadMpvLinux()
	case "windows":
		return downloadMpvWindows()
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func downloadMpvDarwin() (string, error) {
	var url string
	if runtime.GOARCH == "arm64" {
		url = "https://laboratory.stolendata.net/~djinn/mpv_osx/mpv-arm64-latest.tar.gz"
	} else {
		url = "https://laboratory.stolendata.net/~djinn/mpv_osx/mpv-latest.tar.gz"
	}

	tmpFile, err := downloadToTemp(url)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile)

	// Extract mpv binary and libs from the tar.gz
	return extractMpvFromTarGz(tmpFile)
}

func downloadMpvLinux() (string, error) {
	// Get the latest AppImage URL from GitHub API
	url, err := getLatestMpvAppImageURL()
	if err != nil {
		return "", err
	}

	dest := filepath.Join(binDir, "mpv")
	if err := downloadFile(url, dest); err != nil {
		return "", err
	}
	os.Chmod(dest, 0755)
	return dest, nil
}

func downloadMpvWindows() (string, error) {
	// Get the latest release info from GitHub API
	apiURL := "https://api.github.com/repos/shinchiro/mpv-winbuild-cmake/releases/latest"
	resp, err := httpGet(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %w", err)
	}

	// Find the x86_64 7z asset (not -v3-, -gcc-, or -dev-)
	var downloadURL string
	for _, asset := range release.Assets {
		name := asset.Name
		if strings.Contains(name, "mpv-x86_64") &&
			strings.HasSuffix(name, ".7z") &&
			!strings.Contains(name, "-v3-") &&
			!strings.Contains(name, "-gcc-") &&
			!strings.Contains(name, "-dev-") {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return "", fmt.Errorf("could not find mpv Windows build in latest release")
	}

	// Download the 7z file
	tmpFile, err := downloadToTemp(downloadURL)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile)

	// Extract mpv.exe using system 7z if available, otherwise try PowerShell
	dest := filepath.Join(binDir, "mpv.exe")
	if p, err := exec.LookPath("7z"); err == nil {
		cmd := exec.Command(p, "e", "-o"+binDir, tmpFile, "mpv.exe", "-y")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("7z extraction failed: %s: %w", string(out), err)
		}
	} else {
		// Try PowerShell's Expand-Archive (only works for zip, not 7z)
		// Fall back to instructions
		os.Remove(tmpFile)
		return "", fmt.Errorf("mpv download requires 7z to extract. Install 7-Zip and try again, or install mpv manually: https://mpv.io/installation/")
	}

	return dest, nil
}

// --- mpv tar.gz extraction (macOS) ---

func extractMpvFromTarGz(tarGzPath string) (string, error) {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	mpvDest := filepath.Join(binDir, "mpv")
	libDir := filepath.Join(binDir, "lib")
	os.MkdirAll(libDir, 0755)

	foundMpv := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Extract the mpv binary
		if strings.HasSuffix(hdr.Name, "/mpv.app/Contents/MacOS/mpv") && hdr.Typeflag == tar.TypeReg {
			if err := writeFile(mpvDest, tr, 0755); err != nil {
				return "", err
			}
			foundMpv = true
			continue
		}

		// Extract bundled libraries
		if strings.Contains(hdr.Name, "/mpv.app/Contents/MacOS/lib/") && hdr.Typeflag == tar.TypeReg {
			libName := filepath.Base(hdr.Name)
			libPath := filepath.Join(libDir, libName)
			if err := writeFile(libPath, tr, 0755); err != nil {
				return "", err
			}
		}
	}

	if !foundMpv {
		return "", fmt.Errorf("mpv binary not found in archive")
	}
	return mpvDest, nil
}

// --- mpv AppImage URL (Linux) ---

func getLatestMpvAppImageURL() (string, error) {
	apiURL := "https://api.github.com/repos/pkgforge-dev/mpv-AppImage/releases/latest"
	resp, err := httpGet(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %w", err)
	}

	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}

	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, "mpv-") &&
			strings.Contains(asset.Name, arch) &&
			strings.HasSuffix(asset.Name, ".AppImage") {
			return asset.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("could not find mpv AppImage for %s", arch)
}

// --- helpers ---

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tilitili/1.0")
	return http.DefaultClient.Do(req)
}

func downloadFile(url, dest string) error {
	resp, err := httpGet(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func downloadToTemp(url string) (string, error) {
	tmp, err := os.CreateTemp("", "tilitili-download-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()

	if err := downloadFile(url, tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func writeFile(path string, r io.Reader, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}
