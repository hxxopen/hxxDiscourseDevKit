package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxManifestBytes = 64 << 20
	maxArchiveName   = 4096
)

var sourceExcludes = []string{
	"node_modules",
	"vendor/bundle",
	".bundle",
	"tmp",
	"log",
}

type archiveEntry struct {
	Name    string
	Host    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
	SHA256  string
}

type scanRoot struct {
	Host   string
	Prefix string
}

type extracted struct {
	TempDir     string
	HasConfig   bool
	HasData     bool
	HasSource   bool
	HasDatabase bool
	TotalBytes  int64
}

// scanRoots walks the workspace paths and returns archive entries with hashes.
func scanRoots(ctx context.Context, roots []scanRoot, progress func(int, string)) ([]archiveEntry, *Manifest, error) {
	var entries []archiveEntry
	manifest := &Manifest{FormatVersion: FormatVersion}
	symlinks := 0
	for index, root := range roots {
		info, err := os.Stat(root.Host)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, err
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("%w: %s 不是目录", ErrValidation, root.Host)
		}
		walkErr := filepath.WalkDir(root.Host, func(current string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			rel, err := filepath.Rel(root.Host, current)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			slashRel := filepath.ToSlash(rel)
			if root.Prefix == "source" && excludedSourcePath(slashRel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			entry := archiveEntry{
				Name: path.Join(root.Prefix, slashRel),
				Host: current,
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			entry.Mode = info.Mode().Perm()
			entry.ModTime = info.ModTime()
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				symlinks++
				return nil
			case info.IsDir():
				entry.IsDir = true
			case info.Mode().IsRegular():
				hash, size, err := hashFile(current)
				if err != nil {
					return err
				}
				entry.SHA256 = hash
				entry.Size = size
			default:
				return nil
			}
			entries = append(entries, entry)
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
		progress((index+1)*100/len(roots), "扫描 "+root.Prefix)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	manifest.SkippedSymlinks = symlinks
	return entries, manifest, nil
}

func excludedSourcePath(rel string) bool {
	for _, exclude := range sourceExcludes {
		if rel == exclude || strings.HasPrefix(rel, exclude+"/") {
			return true
		}
	}
	return false
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func buildManifestFiles(entries []archiveEntry, database *archiveEntry) []ManifestFile {
	files := make([]ManifestFile, 0, len(entries)+1)
	if database != nil {
		files = append(files, ManifestFile{Path: database.Name, Size: database.Size, SHA256: database.SHA256})
	}
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		files = append(files, ManifestFile{Path: entry.Name, Size: entry.Size, SHA256: entry.SHA256})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// writeArchive writes entries to outPath. The manifest is the first entry and
// each file's hash is verified again while copying.
func writeArchive(ctx context.Context, outPath string, manifest *Manifest, entries []archiveEntry, progress func(int, string)) error {
	file, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(outPath)
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeTarEntry(tarWriter, archiveEntry{
		Name:    "manifest.json",
		Size:    int64(len(manifestBytes)),
		Mode:    0o600,
		ModTime: manifest.CreatedAt,
	}, bytesReader(manifestBytes), manifestBytes); err != nil {
		return err
	}

	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir {
			if err := writeTarDir(tarWriter, entry); err != nil {
				return err
			}
			continue
		}
		source, err := os.Open(entry.Host)
		if err != nil {
			return err
		}
		copyErr := writeTarEntry(tarWriter, entry, source, nil)
		_ = source.Close()
		if copyErr != nil {
			return copyErr
		}
		if index%50 == 0 {
			progress(index*100/maxInt(len(entries), 1), "写入归档")
		}
	}

	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

type byteReaderAt struct {
	data []byte
	pos  int
}

func bytesReader(data []byte) io.Reader { return &byteReaderAt{data: data} }

func (r *byteReaderAt) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func writeTarDir(writer *tar.Writer, entry archiveEntry) error {
	mode := entry.Mode
	if mode == 0 {
		mode = 0o755
	}
	header := &tar.Header{
		Name:     entry.Name + "/",
		Mode:     int64(mode),
		Typeflag: tar.TypeDir,
		ModTime:  entry.ModTime,
	}
	return writer.WriteHeader(header)
}

func writeTarEntry(writer *tar.Writer, entry archiveEntry, source io.Reader, inline []byte) error {
	modTime := entry.ModTime
	if modTime.IsZero() {
		modTime = time.Now().UTC()
	}
	mode := entry.Mode
	if mode == 0 {
		mode = 0o644
	}
	header := &tar.Header{
		Name:     entry.Name,
		Mode:     int64(mode),
		Size:     entry.Size,
		Typeflag: tar.TypeReg,
		ModTime:  modTime,
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	var reader io.Reader = source
	if inline != nil {
		reader = bytesReader(inline)
	}
	hash := sha256.New()
	written, err := io.Copy(writer, io.TeeReader(reader, hash))
	if err != nil {
		return err
	}
	if written != entry.Size {
		return fmt.Errorf("%w: %s 大小在备份过程中发生变化", ErrInvalidArchive, entry.Name)
	}
	if entry.SHA256 != "" {
		if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), entry.SHA256) {
			return fmt.Errorf("%w: %s 内容在备份过程中发生变化", ErrInvalidArchive, entry.Name)
		}
	}
	return nil
}

// readManifest reads only the leading manifest entry.
func readManifest(archivePath string) (*Manifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	if header.Name != "manifest.json" {
		return nil, fmt.Errorf("%w: manifest.json 不是归档的第一个条目", ErrInvalidArchive)
	}
	if header.Size < 0 || header.Size > maxManifestBytes {
		return nil, fmt.Errorf("%w: manifest 过大", ErrInvalidArchive)
	}
	data, err := io.ReadAll(io.LimitReader(tarReader, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest 无法解析: %v", ErrInvalidArchive, err)
	}
	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func validateManifest(manifest *Manifest) error {
	if manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("%w: 不支持的备份格式版本 %d", ErrInvalidArchive, manifest.FormatVersion)
	}
	if manifest.WorkspaceID == "" {
		return fmt.Errorf("%w: manifest 缺少 workspace_id", ErrInvalidArchive)
	}
	for _, file := range manifest.Files {
		if _, err := safeArchiveName(file.Path); err != nil {
			return fmt.Errorf("%w: %s", err, file.Path)
		}
		if len(file.SHA256) != 64 {
			return fmt.Errorf("%w: %s 的 SHA-256 无效", ErrInvalidArchive, file.Path)
		}
		if file.Size < 0 {
			return fmt.Errorf("%w: %s 的大小无效", ErrInvalidArchive, file.Path)
		}
	}
	return nil
}

// verifyArchive reads the entire archive and checks every file hash.
func verifyArchive(archivePath string) (*Manifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)

	var manifest *Manifest
	seen := map[string]bool{}
	first := true
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
		}
		if len(header.Name) > maxArchiveName {
			return nil, fmt.Errorf("%w: 条目名过长", ErrUnsafePath)
		}
		name, err := safeArchiveName(header.Name)
		if err != nil {
			return nil, err
		}
		if first {
			if name != "manifest.json" {
				return nil, fmt.Errorf("%w: manifest.json 不是第一个条目", ErrInvalidArchive)
			}
			first = false
			if header.Size < 0 || header.Size > maxManifestBytes {
				return nil, fmt.Errorf("%w: manifest 过大", ErrInvalidArchive)
			}
			data, readErr := io.ReadAll(io.LimitReader(tarReader, maxManifestBytes+1))
			if readErr != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, readErr)
			}
			manifest = &Manifest{}
			if err := json.Unmarshal(data, manifest); err != nil {
				return nil, fmt.Errorf("%w: manifest 无法解析: %v", ErrInvalidArchive, err)
			}
			if err := validateManifest(manifest); err != nil {
				return nil, err
			}
			continue
		}
		if manifest == nil {
			return nil, fmt.Errorf("%w: 缺少 manifest", ErrInvalidArchive)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("%w: 不支持的条目类型 %q", ErrInvalidArchive, name)
		}
		expected, ok := manifestFile(manifest, name)
		if !ok {
			return nil, fmt.Errorf("%w: 归档包含未登记的文件 %s", ErrInvalidArchive, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: 归档包含重复条目 %s", ErrInvalidArchive, name)
		}
		hash := sha256.New()
		written, err := io.Copy(hash, tarReader)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
		}
		if written != expected.Size {
			return nil, fmt.Errorf("%w: %s 大小不匹配", ErrInvalidArchive, name)
		}
		if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected.SHA256) {
			return nil, fmt.Errorf("%w: %s 校验和不匹配", ErrInvalidArchive, name)
		}
		seen[name] = true
	}
	if manifest == nil {
		return nil, fmt.Errorf("%w: 归档为空", ErrInvalidArchive)
	}
	for _, expected := range manifest.Files {
		if !seen[expected.Path] {
			return nil, fmt.Errorf("%w: 归档缺少文件 %s", ErrInvalidArchive, expected.Path)
		}
	}
	return manifest, nil
}

func manifestFile(manifest *Manifest, name string) (ManifestFile, bool) {
	for _, file := range manifest.Files {
		if file.Path == name {
			return file, true
		}
	}
	return ManifestFile{}, false
}

// extractArchive verifies and extracts the restorable parts of the archive
// into tempDir. The workspace metadata is validated but not extracted.
func extractArchive(ctx context.Context, archivePath, tempDir string, progress func(int, string)) (*extracted, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)

	result := &extracted{TempDir: tempDir}
	var manifest *Manifest
	seen := map[string]bool{}
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
		}
		name, err := safeArchiveName(header.Name)
		if err != nil {
			return nil, err
		}
		if first && name != "manifest.json" {
			return nil, fmt.Errorf("%w: manifest.json 不是第一个条目", ErrInvalidArchive)
		}
		first = false
		if name == "manifest.json" {
			data, readErr := io.ReadAll(io.LimitReader(tarReader, maxManifestBytes+1))
			if readErr != nil {
				return nil, readErr
			}
			manifest = &Manifest{}
			if err := json.Unmarshal(data, manifest); err != nil {
				return nil, fmt.Errorf("%w: manifest 无法解析: %v", ErrInvalidArchive, err)
			}
			if err := validateManifest(manifest); err != nil {
				return nil, err
			}
			continue
		}
		if manifest == nil {
			return nil, fmt.Errorf("%w: 缺少 manifest", ErrInvalidArchive)
		}
		relative, kind, ok := restoreRelativePath(name)
		if !ok {
			return nil, fmt.Errorf("%w: 归档包含无法恢复的路径 %s", ErrUnsafePath, name)
		}
		switch kind {
		case "metadata":
			// The current workspace identity is kept, but the archived copy is
			// still verified so corruption is detected before any change.
			if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 8<<20 {
				return nil, fmt.Errorf("%w: workspace.json 无效", ErrInvalidArchive)
			}
			expected, known := manifestFile(manifest, name)
			if !known {
				return nil, fmt.Errorf("%w: 归档包含未登记的 workspace.json", ErrInvalidArchive)
			}
			hash := sha256.New()
			written, copyErr := io.Copy(hash, tarReader)
			if copyErr != nil {
				return nil, copyErr
			}
			if written != expected.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected.SHA256) {
				return nil, fmt.Errorf("%w: workspace.json 校验和不匹配", ErrInvalidArchive)
			}
			seen[name] = true
			progress(60, "校验 workspace.json")
			continue
		case "database":
			result.HasDatabase = true
		case "config":
			result.HasConfig = true
		case "data":
			result.HasData = true
		case "source":
			result.HasSource = true
		}
		target, err := safeJoin(tempDir, relative)
		if err != nil {
			return nil, err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("%w: 不支持的条目类型 %q", ErrInvalidArchive, name)
		}
		expected, known := manifestFile(manifest, name)
		if !known {
			return nil, fmt.Errorf("%w: 归档包含未登记的文件 %s", ErrInvalidArchive, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: 归档包含重复条目 %s", ErrInvalidArchive, name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		mode := fs.FileMode(header.Mode) & 0o777
		if mode == 0 {
			mode = 0o644
		}
		destination, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(destination, hash), tarReader)
		closeErr := destination.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if written != expected.Size {
			return nil, fmt.Errorf("%w: %s 大小不匹配", ErrInvalidArchive, name)
		}
		if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected.SHA256) {
			return nil, fmt.Errorf("%w: %s 校验和不匹配", ErrInvalidArchive, name)
		}
		seen[name] = true
		result.TotalBytes += written
		progress(60, "解压 "+name)
	}
	if manifest == nil {
		return nil, fmt.Errorf("%w: 归档为空", ErrInvalidArchive)
	}
	for _, expected := range manifest.Files {
		if !seen[expected.Path] {
			return nil, fmt.Errorf("%w: 归档缺少文件 %s", ErrInvalidArchive, expected.Path)
		}
	}
	return result, nil
}

// restoreRelativePath maps an archive entry to a path relative to the
// workspace directory. The returned kind identifies the restored section.
func restoreRelativePath(name string) (relative, kind string, ok bool) {
	switch name {
	case "manifest.json":
		return "", "manifest", false
	case "workspace/workspace.json":
		return "", "metadata", true
	case "database.dump":
		return "database.dump", "database", true
	}
	for prefix, target := range map[string]string{
		"workspace/config": "config",
		"workspace/data":   "data",
		"source":           "source",
	} {
		if name == prefix {
			return target, sectionKind(target), true
		}
		if strings.HasPrefix(name, prefix+"/") {
			return path.Join(target, strings.TrimPrefix(name, prefix+"/")), sectionKind(target), true
		}
	}
	return "", "", false
}

func sectionKind(target string) string {
	switch target {
	case "config":
		return "config"
	case "data":
		return "data"
	default:
		return "source"
	}
}

func safeArchiveName(name string) (string, error) {
	if name == "" || len(name) > maxArchiveName {
		return "", fmt.Errorf("%w: 条目名为空或过长", ErrUnsafePath)
	}
	if strings.ContainsAny(name, "\x00\\:") {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, name)
	}
	clean := path.Clean(trimmed)
	if clean != trimmed || clean == "." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, name)
	}
	return clean, nil
}

func safeJoin(root, relative string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(relative))
	check, err := filepath.Rel(root, target)
	if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(os.PathSeparator)) ||
		filepath.IsAbs(check) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, relative)
	}
	return target, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
