package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// PastedImage represents an image retrieved from the clipboard.
type PastedImage struct {
	Bytes       []byte
	ContentType string // e.g. "image/png", "image/jpeg"
}

// localImagePathsFromPaste returns local image files only when the entire
// pasted payload consists of valid paths. Ordinary pasted text is untouched.
func localImagePathsFromPaste(raw string) []string {
	if path, ok := normalizePastedImagePath(raw); ok {
		return []string{path}
	}

	lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' })
	if len(lines) < 2 {
		return nil
	}
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		path, ok := normalizePastedImagePath(line)
		if !ok {
			return nil
		}
		paths = append(paths, path)
	}
	return paths
}

func normalizePastedImagePath(raw string) (string, bool) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", false
	}
	if unquoted, err := strconv.Unquote(path); err == nil {
		path = unquoted
	} else {
		path = strings.Trim(path, "'\"")
	}
	if strings.HasPrefix(path, "file://") {
		parsed, err := url.Parse(path)
		if err != nil || parsed.Host != "" && parsed.Host != "localhost" {
			return "", false
		}
		path, err = url.PathUnescape(parsed.Path)
		if err != nil {
			return "", false
		}
	}
	path = strings.ReplaceAll(path, `\ `, " ")
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	path = filepath.Clean(path)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".heic", ".heif":
	default:
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// GetClipboardImage tries to read an image from the clipboard.
// It supports Linux (wl-paste, xclip), macOS (osascript), and Windows (powershell).
func GetClipboardImage() ([]byte, string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxClipboardImage()
	case "darwin":
		return getMacClipboardImage()
	case "windows":
		return getWindowsClipboardImage()
	default:
		return nil, "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func getLinuxClipboardImage() ([]byte, string, error) {
	// 1. Try wl-paste (Wayland)
	if _, err := exec.LookPath("wl-paste"); err == nil {
		// Try PNG
		cmd := exec.Command("wl-paste", "-t", "image/png")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/png", nil
		}
		// Try JPEG
		cmd = exec.Command("wl-paste", "-t", "image/jpeg")
		out.Reset()
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/jpeg", nil
		}
	}

	// 2. Try xclip (X11)
	if _, err := exec.LookPath("xclip"); err == nil {
		// Try PNG
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/png", nil
		}
		// Try JPEG
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/jpeg", "-o")
		out.Reset()
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/jpeg", nil
		}
	}

	return nil, "", errors.New("no clipboard image found or required CLI tools (wl-paste, xclip) are missing")
}

func getMacClipboardImage() ([]byte, string, error) {
	// Try PNG using osascript
	cmd := exec.Command("osascript", "-e", "get the clipboard as «class PNGf»")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		s := strings.TrimSpace(out.String())
		// Output is format like: «data PNGf89504E47...»
		s = strings.TrimPrefix(s, "«data PNGf")
		s = strings.TrimSuffix(s, "»")
		if data, err := hex.DecodeString(s); err == nil && len(data) > 0 {
			return data, "image/png", nil
		}
	}

	// Try JPEG/TIFF as fallback
	cmd = exec.Command("osascript", "-e", "get the clipboard as «class JPEG»")
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		s := strings.TrimSpace(out.String())
		s = strings.TrimPrefix(s, "«data JPEG")
		s = strings.TrimSuffix(s, "»")
		if data, err := hex.DecodeString(s); err == nil && len(data) > 0 {
			return data, "image/jpeg", nil
		}
	}

	return nil, "", errors.New("no clipboard image found on macOS")
}

func getWindowsClipboardImage() ([]byte, string, error) {
	psCmd := "[void][System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms'); " +
		"$img = [System.Windows.Forms.Clipboard]::GetImage(); " +
		"if ($img -ne $null) { " +
		"  $ms = New-Object System.IO.MemoryStream; " +
		"  $img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); " +
		"  [System.BitConverter]::ToString($ms.ToArray()) -replace '-','' " +
		"}"

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, "", err
	}

	s := strings.TrimSpace(out.String())
	if len(s) == 0 {
		return nil, "", errors.New("no clipboard image found on Windows")
	}

	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, "", err
	}

	return data, "image/png", nil
}
