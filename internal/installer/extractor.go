package installer

import (
	"archive/tar"    // For reading .tar archives
	"archive/zip"    // For reading .zip archives
	"compress/bzip2" // For reading .bz2 compressed data
	"compress/gzip"  // For reading .gz compressed data
	"fmt"
	"github.com/bodgit/sevenzip" // For reading .7z archives
	"github.com/xi2/xz"          // For reading .xz compressed data
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"setup-machine/internal/logger"
	"strings"
)

// ExtractAndInstall extracts an archive and installs its binary/binaries into /usr/local/bin or fallback $HOME/bin
func ExtractAndInstall(src, dest string) (string, error) {
	// Extract the archive to the destination
	extractedPath, err := ExtractArchive(src, dest)
	if err != nil {
		return "", err
	}

	// Get info about the extracted path
	info, err := os.Stat(extractedPath)
	if err != nil {
		return "", err
	}

	// Infer tool name from source archive filename
	toolName := extractToolNameFromPath(src)

	var binaries []string
	// If extracted path is a directory, scan for binaries
	if info.IsDir() {
		binaries, err = findExecutables(extractedPath, toolName)
		if err != nil || len(binaries) == 0 {
			return "", fmt.Errorf("no binary found in folder: %w", err)
		}
	} else {
		// If it's a single file, assume it's the binary
		binaries = []string{extractedPath}
	}

	// Try to copy binaries to /usr/local/bin
	destination := "/usr/local/bin"
	for _, binaryPath := range binaries {
		if err := copyBinary(binaryPath, destination); err != nil {
			// If /usr/local/bin fails, fallback to ~/bin
			homeBin := filepath.Join(os.Getenv("HOME"), "bin")
			if err := os.MkdirAll(homeBin, 0755); err != nil {
				return "", fmt.Errorf("cannot create fallback bin directory: %w", err)
			}
			destination = homeBin
			if err := copyBinary(binaryPath, homeBin); err != nil {
				return "", fmt.Errorf("failed to copy binary to fallback location: %w", err)
			}
		}
	}

	finalPath := filepath.Join(destination, filepath.Base(binaries[0]))
	return finalPath, nil
}

// extractToolNameFromPath attempts to derive a reasonable tool name from a given archive path
func extractToolNameFromPath(path string) string {
	filename := filepath.Base(path)

	// Strip known archive extensions
	for _, ext := range []string{".tar.gz", ".tgz", ".tar.bz2", ".tar.xz", ".zip", ".7z"} {
		if strings.HasSuffix(filename, ext) {
			filename = strings.TrimSuffix(filename, ext)
			break
		}
	}

	// Split on delimiters like "-" or "_" and return the first part
	parts := strings.FieldsFunc(filename, func(r rune) bool {
		return r == '-' || r == '_'
	})

	if len(parts) > 0 {
		return parts[0]
	}
	return filename
}

// ExtractArchive routes to appropriate extraction function based on archive type
func ExtractArchive(src, dest string) (string, error) {
	switch {
	case strings.HasSuffix(src, ".zip"):
		logger.Debug("[Debug] compression type is zip")
		return extractZip(src, dest)
	case strings.HasSuffix(src, ".7z"):
		logger.Debug("[Debug] compression type is .7z")
		return extract7z(src, dest)
	case strings.HasSuffix(src, ".tar"), strings.HasSuffix(src, ".tar.gz"), strings.HasSuffix(src, ".tgz"),
		strings.HasSuffix(src, ".tar.bz2"), strings.HasSuffix(src, ".tar.xz"):
		logger.Debug("[Debug] compression type is .tar.*")
		return extractTarArchive(src, dest)
	default:
		return "", fmt.Errorf("unsupported archive format: %s", src)
	}
}

// extractTarArchive handles tar and compressed tar variants
func extractTarArchive(src, dest string) (string, error) {
	logger.Debug("[Debug] uncompressing  %s to %s\n", src, dest)
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var reader io.Reader = f
	switch {
	case strings.HasSuffix(src, ".tar.gz"), strings.HasSuffix(src, ".tgz"):
		gr, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gr.Close()
		reader = gr
	case strings.HasSuffix(src, ".tar.bz2"):
		reader = bzip2.NewReader(f)
	case strings.HasSuffix(src, ".tar.xz"):
		xzr, err := xz.NewReader(f, 0)
		if err != nil {
			return "", err
		}
		reader = xzr
	}

	tr := tar.NewReader(reader)
	var topLevel string

	// Iterate over each file in the archive
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", err
		}

		// Capture the top-level folder name
		if topLevel == "" {
			parts := strings.Split(hdr.Name, string(os.PathSeparator))
			if len(parts) > 0 {
				topLevel = parts[0]
			}
		}

		target := filepath.Join(dest, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", err
			}
			outFile, err := os.Create(target)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
		}
	}
	return filepath.Join(dest, topLevel), nil
}

// extractZip extracts a .zip archive
func extractZip(src, dest string) (string, error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return "", err
	}
	defer r.Close()

	var topLevel string
	for _, f := range r.File {
		path := filepath.Join(dest, f.Name)
		if topLevel == "" {
			parts := strings.Split(f.Name, string(os.PathSeparator))
			if len(parts) > 0 {
				topLevel = parts[0]
			}
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return "", err
		}
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dest, topLevel), nil
}

// extract7z handles .7z extraction using the sevenzip library
func extract7z(src, dest string) (string, error) {
	r, err := sevenzip.OpenReader(src)
	if err != nil {
		return "", fmt.Errorf("failed to open 7z archive: %w", err)
	}
	defer r.Close()

	var topLevel string
	for _, f := range r.File {
		path := filepath.Join(dest, f.Name)
		if topLevel == "" {
			parts := strings.Split(f.Name, string(os.PathSeparator))
			if len(parts) > 0 {
				topLevel = parts[0]
			}
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		outFile, err := os.Create(path)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dest, topLevel), nil
}

// findExecutables scans a directory tree and returns all executable files matching the tool name
func findExecutables(root string, toolName string) ([]string, error) {
	logger.Debug("[DEBUG] Scanning directory for executables: %s", root)
	var executables []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			logger.Debug("[DEBUG] WalkDir error: %v", err)
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			logger.Debug("[DEBUG] Failed to get file info for %s: %v", path, err)
			return nil
		}
		mode := info.Mode()
		filename := filepath.Base(path)

		// Skip if filename doesn't start with toolName
		if !strings.HasPrefix(filename, toolName) {
			return nil
		}

		// Check if it’s executable based on permissions
		if mode.IsRegular() && (mode.Perm()&0111 != 0 || strings.HasPrefix(mode.String(), "-rwx")) {
			logger.Debug("[DEBUG] Found executable (perm): %s", path)
			executables = append(executables, path)
			return nil
		}

		// Fallback: use `file` command to determine if it’s executable
		out, err := exec.Command("file", "--brief", path).Output()
		if err != nil {
			return nil
		}
		output := strings.ToLower(string(out))
		if strings.Contains(output, "executable") || strings.Contains(output, "mach-o") || strings.Contains(output, "elf") {
			logger.Debug("[DEBUG] Found executable via file command: %s", path)
			executables = append(executables, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	if len(executables) == 0 {
		return nil, fmt.Errorf("no executables found in %s", root)
	}
	return executables, nil
}

// copyBinary copies a file to a target directory with executable permissions
func copyBinary(src, dstDir string) error {
	dst := filepath.Join(dstDir, filepath.Base(src))
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
