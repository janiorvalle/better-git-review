package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/gitexec"
)

const (
	MaxPreviewBytes      = 1536 * 1024
	MaxTotalPreviewBytes = 12 * 1024 * 1024
)

type Settings struct {
	MaxPreviewBytes      int64
	MaxTotalPreviewBytes int64
	ImageExtensions      []string
}

func DefaultSettings() Settings {
	return Settings{MaxPreviewBytes, MaxTotalPreviewBytes, []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg"}}
}

type Source struct {
	RepoDir string
	BaseRef string
	HeadRef string
	Dirty   bool
}

type Asset struct {
	DataURI    string
	Size       int64
	SizeLabel  string
	Dimensions string
}

type Preview struct {
	Image bool
	Old   *Asset
	New   *Asset
	Label string
}

func Enrich(ctx context.Context, files []document.File, source Source, runner gitexec.Runner) map[int]Preview {
	return EnrichWithSettings(ctx, files, source, runner, DefaultSettings())
}

func EnrichWithSettings(ctx context.Context, files []document.File, source Source, runner gitexec.Runner, settings Settings) map[int]Preview {
	result := map[int]Preview{}
	if runner == nil {
		runner = gitexec.ExecRunner{}
	}
	baseRef := source.BaseRef
	if !source.Dirty && baseRef != "" && source.HeadRef != "" {
		if merged, err := runner.Run(ctx, source.RepoDir, "merge-base", baseRef, source.HeadRef); err == nil {
			baseRef = strings.TrimSpace(string(merged))
		}
	}
	remainingPreviewBytes := settings.MaxTotalPreviewBytes
	for index, file := range files {
		if !file.Binary {
			continue
		}
		imageFile := isImageWithExtensions(file.Path, settings.ImageExtensions)
		if source.RepoDir == "" {
			label := "Binary file \u00b7 content not available from patch input."
			if imageFile {
				label = "Binary image \u00b7 content not available from patch input."
			}
			result[index] = Preview{Image: imageFile, Label: label}
			continue
		}
		var oldAsset *Asset
		if file.Status != "added" {
			oldAsset = loadGitAsset(ctx, source.RepoDir, baseRef, file.OldPath, runner, imageFile, remainingPreviewBytes, settings.MaxPreviewBytes)
			remainingPreviewBytes -= embeddedBytes(oldAsset)
		}
		var newAsset *Asset
		if file.Status != "deleted" {
			if source.Dirty {
				newAsset = loadWorktreeAsset(source.RepoDir, file.NewPath, imageFile, remainingPreviewBytes, settings.MaxPreviewBytes)
			} else {
				newAsset = loadGitAsset(ctx, source.RepoDir, source.HeadRef, file.NewPath, runner, imageFile, remainingPreviewBytes, settings.MaxPreviewBytes)
			}
			remainingPreviewBytes -= embeddedBytes(newAsset)
		}
		label := sizeStory(imageFile, file.Status, oldAsset, newAsset)
		if imageFile && canPreview(file.Status, oldAsset, newAsset) {
			result[index] = Preview{Image: true, Old: oldAsset, New: newAsset, Label: label}
			continue
		}
		result[index] = Preview{Image: imageFile, Label: label}
	}
	return result
}

func loadGitAsset(ctx context.Context, repoDir, ref, path string, runner gitexec.Runner, includeContent bool, contentBudget int64, limits ...int64) *Asset {
	maxPreviewBytes := int64(MaxPreviewBytes)
	if len(limits) > 0 {
		maxPreviewBytes = limits[0]
	}
	if ref == "" || ref == "WORKTREE" || path == "" || path == "/dev/null" {
		return nil
	}
	spec := ref + ":" + path
	sizeRaw, err := runner.Run(ctx, repoDir, "cat-file", "-s", spec)
	if err != nil {
		return nil
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeRaw)), 10, 64)
	if err != nil {
		return nil
	}
	asset := &Asset{Size: size, SizeLabel: formatSize(size)}
	if !includeContent || size > maxPreviewBytes || size > contentBudget {
		return asset
	}
	content, err := runner.Run(ctx, repoDir, "cat-file", "-p", spec)
	if err != nil || int64(len(content)) != size {
		return asset
	}
	fillImage(asset, path, content)
	return asset
}

func loadWorktreeAsset(repoDir, path string, includeContent bool, contentBudget int64, limits ...int64) *Asset {
	maxPreviewBytes := int64(MaxPreviewBytes)
	if len(limits) > 0 {
		maxPreviewBytes = limits[0]
	}
	fullPath, pathInfo, ok := safeWorktreePath(repoDir, path)
	if !ok {
		return nil
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		return nil
	}
	asset := &Asset{Size: info.Size(), SizeLabel: formatSize(info.Size())}
	if !includeContent || info.Size() > maxPreviewBytes || info.Size() > contentBudget {
		return asset
	}
	content, err := io.ReadAll(io.LimitReader(file, maxPreviewBytes+1))
	if err != nil || int64(len(content)) > maxPreviewBytes || int64(len(content)) != info.Size() {
		return asset
	}
	fillImage(asset, path, content)
	return asset
}

func embeddedBytes(asset *Asset) int64 {
	if asset == nil || asset.DataURI == "" {
		return 0
	}
	return asset.Size
}

func safeWorktreePath(repoDir, path string) (string, os.FileInfo, bool) {
	root, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		return "", nil, false
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", nil, false
	}
	full, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return "", nil, false
	}
	relative, err := filepath.Rel(root, full)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, false
	}
	current := root
	var info os.FileInfo
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err = os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, false
		}
	}
	if info == nil || !info.Mode().IsRegular() {
		return "", nil, false
	}
	return full, info, true
}

func fillImage(asset *Asset, path string, content []byte) {
	mime := imageMIME(path)
	asset.DataURI = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)
	if width, height, ok := dimensions(path, content); ok {
		asset.Dimensions = fmt.Sprintf("%d x %d", width, height)
	}
}

func previewable(asset *Asset) bool {
	return asset == nil || asset.DataURI != ""
}

func canPreview(status string, oldAsset, newAsset *Asset) bool {
	switch status {
	case "added":
		return newAsset != nil && previewable(newAsset)
	case "deleted":
		return oldAsset != nil && previewable(oldAsset)
	default:
		return oldAsset != nil && newAsset != nil && previewable(oldAsset) && previewable(newAsset)
	}
}

func sizeStory(imageFile bool, status string, oldAsset, newAsset *Asset) string {
	kind := "Binary file"
	if imageFile {
		kind = "Binary image"
	}
	if (status == "added" && newAsset == nil) || (status == "deleted" && oldAsset == nil) ||
		(status != "added" && status != "deleted" && (oldAsset == nil || newAsset == nil)) {
		return kind + " · one or more sides unavailable"
	}
	if oldAsset == nil && newAsset == nil {
		return kind + " \u00b7 content unavailable"
	}
	if oldAsset == nil {
		return fmt.Sprintf("%s \u00b7 added %s", kind, newAsset.SizeLabel)
	}
	if newAsset == nil {
		return fmt.Sprintf("%s \u00b7 deleted %s", kind, oldAsset.SizeLabel)
	}
	delta := newAsset.Size - oldAsset.Size
	return fmt.Sprintf("%s \u00b7 %s \u2192 %s (%s)", kind, oldAsset.SizeLabel, newAsset.SizeLabel, formatDelta(delta))
}

func formatSize(size int64) string {
	if size < 1000 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1_000_000 {
		return fmt.Sprintf("%.1f KB", float64(size)/1000)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/1_000_000)
}

func formatDelta(delta int64) string {
	sign := "+"
	if delta < 0 {
		sign = "-"
		delta = -delta
	}
	return sign + formatSize(delta)
}

func isImage(path string) bool {
	return isImageWithExtensions(path, DefaultSettings().ImageExtensions)
}

func isImageWithExtensions(path string, extensions []string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	for _, configured := range extensions {
		if extension == strings.ToLower(configured) {
			return true
		}
	}
	return false
}

func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

func dimensions(path string, content []byte) (int, int, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".svg" {
		return svgDimensions(content)
	}
	if ext == ".webp" {
		return webpDimensions(content)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

func svgDimensions(content []byte) (int, int, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		token, err := decoder.Token()
		if err != nil {
			return 0, 0, false
		}
		start, ok := token.(xml.StartElement)
		if !ok || strings.ToLower(start.Name.Local) != "svg" {
			continue
		}
		var width, height int
		var viewBox string
		for _, attr := range start.Attr {
			value := strings.TrimSuffix(strings.TrimSpace(attr.Value), "px")
			switch strings.ToLower(attr.Name.Local) {
			case "width":
				width, _ = strconv.Atoi(value)
			case "height":
				height, _ = strconv.Atoi(value)
			case "viewbox":
				viewBox = attr.Value
			}
		}
		if (width == 0 || height == 0) && viewBox != "" {
			parts := strings.Fields(strings.ReplaceAll(viewBox, ",", " "))
			if len(parts) == 4 {
				viewWidth, widthErr := strconv.ParseFloat(parts[2], 64)
				viewHeight, heightErr := strconv.ParseFloat(parts[3], 64)
				if widthErr == nil && heightErr == nil {
					width, height = int(math.Round(viewWidth)), int(math.Round(viewHeight))
				}
			}
		}
		return width, height, width > 0 && height > 0
	}
}

func webpDimensions(content []byte) (int, int, bool) {
	if len(content) < 30 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WEBP" {
		return 0, 0, false
	}
	if string(content[12:16]) == "VP8X" {
		width := int(content[24]) | int(content[25])<<8 | int(content[26])<<16
		height := int(content[27]) | int(content[28])<<8 | int(content[29])<<16
		return width + 1, height + 1, true
	}
	if string(content[12:16]) == "VP8 " && len(content) >= 30 {
		return int(binary.LittleEndian.Uint16(content[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(content[28:30]) & 0x3fff), true
	}
	if string(content[12:16]) == "VP8L" && len(content) >= 25 && content[20] == 0x2f {
		bits := binary.LittleEndian.Uint32(content[21:25])
		width := int(bits&0x3fff) + 1
		height := int((bits>>14)&0x3fff) + 1
		return width, height, true
	}
	return 0, 0, false
}
