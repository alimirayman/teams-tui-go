package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/nfnt/resize"
)

// isImageAttachment checks if the attachment is an image based on ContentType or file extension.
func isImageAttachment(att MessageAttachment) bool {
	if att.ContentType != nil && strings.HasPrefix(strings.ToLower(*att.ContentType), "image/") {
		return true
	}
	if att.Name != nil {
		ext := strings.ToLower(filepath.Ext(*att.Name))
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" {
			return true
		}
	}
	if att.ContentURL != nil {
		ext := strings.ToLower(filepath.Ext(*att.ContentURL))
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" {
			return true
		}
	}
	return false
}

// getAttachmentCachePath returns the local cached path for an attachment.
func getAttachmentCachePath(att MessageAttachment) (string, error) {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	previewsDir := filepath.Join(cacheDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o700); err != nil {
		return "", err
	}

	var urlStr string
	if att.ContentURL != nil {
		urlStr = *att.ContentURL
	} else if att.Content != nil {
		urlStr = *att.Content
	} else {
		urlStr = att.ID
	}

	hash := sha256.Sum256([]byte(urlStr))
	hashStr := hex.EncodeToString(hash[:])

	ext := ".png"
	if att.Name != nil {
		if e := filepath.Ext(*att.Name); e != "" {
			ext = e
		}
	}

	return filepath.Join(previewsDir, hashStr+ext), nil
}

// MsgPreviewDownloaded is sent when a background preview image download completes.
type MsgPreviewDownloaded struct {
	DestPath string
	Err      error
}

// downloadPreviewCmd downloads a file attachment to cache silently.
func downloadPreviewCmd(clientID, fileURL, destPath string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgPreviewDownloaded{Err: err}
		}
		err = DownloadFile(token, fileURL, destPath)
		return MsgPreviewDownloaded{DestPath: destPath, Err: err}
	}
}

// kittyImageSequence generates the escape sequence to draw a high-quality centered image using Kitty Graphics Protocol.
func kittyImageSequence(filePath string, x, y, cols, rows int) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	img, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return ""
	}

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	// 1. Get exact cell size in pixels
	cellW, cellH := getCellSize()

	// 2. Compute maximum available pixel dimensions
	maxPixelW := cols * cellW
	maxPixelH := rows * cellH

	// 3. Scale original dimensions to fit within maximum pixel bounds
	scaleX := float64(maxPixelW) / float64(origW)
	scaleY := float64(maxPixelH) / float64(origH)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	newPixelW := int(float64(origW) * scale)
	newPixelH := int(float64(origH) * scale)
	if newPixelW < 1 {
		newPixelW = 1
	}
	if newPixelH < 1 {
		newPixelH = 1
	}

	// 4. Calculate cell columns and rows occupied by the new pixel dimensions
	c := int(float64(newPixelW)/float64(cellW) + 0.5)
	r := int(float64(newPixelH)/float64(cellH) + 0.5)
	if c < 1 {
		c = 1
	}
	if r < 1 {
		r = 1
	}
	if c > cols {
		c = cols
	}
	if r > rows {
		r = rows
	}

	// 5. Center the image inside the border box
	padX := (cols - c) / 2
	padY := (rows - r) / 2
	targetX := x + padX
	targetY := y + padY

	// 6. Resample the image client-side to the exact target pixels using high-quality Lanczos3
	resizedImg := resize.Resize(uint(newPixelW), uint(newPixelH), img, resize.Lanczos3)

	// 7. Encode as PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, resizedImg); err != nil {
		return ""
	}
	pngBytes := buf.Bytes()
	encoded := base64.StdEncoding.EncodeToString(pngBytes)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\x1b[s\x1b[%d;%dH", targetY+1, targetX+1))

	const chunkSize = 4096
	totalLen := len(encoded)

	for i := 0; i < totalLen; i += chunkSize {
		end := i + chunkSize
		mVal := 1
		if end >= totalLen {
			end = totalLen
			mVal = 0
		}

		chunk := encoded[i:end]
		if i == 0 {
			sb.WriteString(fmt.Sprintf("\x1b_Ga=T,f=100,m=%d,c=%d,r=%d;%s\x1b\\", mVal, c, r, chunk))
		} else {
			sb.WriteString(fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", mVal, chunk))
		}
	}

	sb.WriteString("\x1b[u")
	return sb.String()
}

// clearKittyImagesCmd returns a Bubble Tea command to clear all displayed Kitty images.
func clearKittyImagesCmd() tea.Cmd {
	return func() tea.Msg {
		_ = writeTerminalSequence(os.Stdout, "\x1b_Ga=d,d=a\x1b\\")
		return nil
	}
}

// previewImage is used by the CLI subcommand "preview-image"
func previewImage(path string) {
	seq := kittyImageSequence(path, 0, 0, 80, 24)
	if seq != "" {
		fmt.Printf("\x1b_Ga=d,d=a\x1b\\%s\n", seq)
	}
	fmt.Println("Image preview loaded (Kitty Graphics Protocol). Press Enter to exit...")
	_, _ = os.Stdin.Read(make([]byte, 1))
}
