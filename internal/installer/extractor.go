package installer

import (
	"archive/tar"    // Package to read tar archives
	"archive/zip"    // Package to read zip archives
	"compress/bzip2" // Package to read bzip2 compressed data streams
	"compress/gzip"  // Package to read gzip compressed data streams
	"fmt"
	"github.com/bodgit/sevenzip" // Third-party package to read 7z archives
	"github.com/xi2/xz"          // Third-party package to read xz compressed streams
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"setup-machine/internal/config"
	"strings"
)

// extractAndInstall extracts an archive file from 'src' into 'dest' directory,
// then locates executable binaries and copies them to /usr/local/bin or ~/bin.
// Returns the final installed binary path or an error.
func extractAndInstall(src, dest string) (string, error) {
	// First extract the archive file to the destination folder.
	extractedPath, err := extractArchive(src, dest)
	if err != nil {
		// Return early if extraction fails.
		return "", err
	}

	// Get file or directory info of the extracted path to check if it is a directory.
	info, err := os.Stat(extractedPath)
	if err != nil {
		return "", err
	}

	// Deduce a likely tool name from the archive filename to help find binaries.
	toolName := extractToolNameFromPath(src)

	var binaries []string
	if info.IsDir() {
		// If extraction produced a directory, scan recursively for executables
		// whose names start with the inferred toolName.
		binaries, err = findExecutables(extractedPath, toolName)
		if err != nil || len(binaries) == 0 {
			return "", fmt.Errorf("no binary found in folder: %w", err)
		}
	} else {
		// If extraction is a single file, assume that is the binary to install.
		binaries = []string{extractedPath}
	}

	// Attempt to copy each binary to /usr/local/bin (common executable path)
	destination := "/usr/local/bin"

	for _, binaryPath := range binaries {
		dstPath := filepath.Join(destination, filepath.Base(binaryPath))

		// Attempt to copy with mode override set to 0755
		if err := copyFile(binaryPath, dstPath, 0755); err != nil {
			config.Warn("[WARN] Primary copy failed (%s): %v. Falling back to ~/bin\n", dstPath, err)

			// Fallback to $HOME/bin
			homeBin := filepath.Join(os.Getenv("HOME"), "bin")
			if err := os.MkdirAll(homeBin, 0755); err != nil {
				return "", fmt.Errorf("cannot create fallback bin directory: %w", err)
			}
			dstPath = filepath.Join(homeBin, filepath.Base(binaryPath))

			if err := copyFile(binaryPath, dstPath, 0755); err != nil {
				return "", fmt.Errorf("failed to copy binary to fallback location: %w", err)
			}
			destination = homeBin
		}
	}

	// Return full path to the first installed binary as the final installed tool path.
	finalPath := filepath.Join(destination, filepath.Base(binaries[0]))
	return finalPath, nil
}

// extractToolNameFromPath attempts to guess a tool name based on archive filename,
// stripping common archive extensions and splitting on delimiters.
func extractToolNameFromPath(path string) string {
	filename := filepath.Base(path)

	// Remove common archive extensions like .tar.gz, .zip, .7z to get base name.
	for _, ext := range []string{".tar.gz", ".tgz", ".tar.bz2", ".tar.xz", ".zip", ".7z"} {
		if strings.HasSuffix(filename, ext) {
			filename = strings.TrimSuffix(filename, ext)
			break
		}
	}

	// Split filename on dashes or underscores, often used as separators,
	// and return the first part as the tool name.
	parts := strings.FieldsFunc(filename, func(r rune) bool {
		return r == '-' || r == '_'
	})

	if len(parts) > 0 {
		return parts[0]
	}
	// Fallback to full filename if no delimiters found.
	return filename
}

// extractArchive routes the archive file to the correct extraction function
// depending on the file extension.
func extractArchive(src, dest string) (string, error) {
	switch {
	case strings.HasSuffix(src, ".zip"):
		config.Debug("[Debug] compression type is zip\n")
		return extractZip(src, dest)
	case strings.HasSuffix(src, ".7z"):
		config.Debug("[Debug] compression type is .7z\n")
		return extract7z(src, dest)
	case strings.HasSuffix(src, ".tar"), strings.HasSuffix(src, ".tar.gz"), strings.HasSuffix(src, ".tgz"),
		strings.HasSuffix(src, ".tar.bz2"), strings.HasSuffix(src, ".tar.xz"):
		config.Debug("[Debug] compression type is .tar.*\n")
		return extractTarArchive(src, dest)
	default:
		// Unsupported archive type
		return "", fmt.Errorf("unsupported archive format: %s\n", src)
	}
}

// extractTarArchive handles extraction of tar archives and their compressed variants
// including .tar.gz, .tgz, .tar.bz2, and .tar.xz
func extractTarArchive(src, dest string) (string, error) {
	config.Debug("[Debug] uncompressing  %s to %s\n", src, dest)

	// Open the source archive file for reading.
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Reader interface for reading decompressed data from the archive.
	var reader io.Reader = f

	// Detect compression type by file extension and wrap the reader accordingly.
	switch {
	case strings.HasSuffix(src, ".tar.gz"), strings.HasSuffix(src, ".tgz"):
		gr, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gr.Close()
		reader = gr
	case strings.HasSuffix(src, ".tar.bz2"):
		// bzip2.NewReader does not require Close()
		reader = bzip2.NewReader(f)
	case strings.HasSuffix(src, ".tar.xz"):
		xzr, err := xz.NewReader(f, 0)
		if err != nil {
			return "", err
		}
		reader = xzr
	}

	// Create a tar.Reader to iterate over files inside the tar archive.
	tr := tar.NewReader(reader)

	var topLevel string // To record top-level folder or file extracted

	// Iterate through each file or directory in the tar archive.
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive reached
		}
		if err != nil {
			return "", err // Error while reading archive
		}

		// On the first file, capture the top-level directory or file name
		if topLevel == "" {
			parts := strings.Split(hdr.Name, string(os.PathSeparator))
			if len(parts) > 0 {
				topLevel = parts[0]
			}
		}

		// Construct the full target path for extraction.
		target := filepath.Join(dest, hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create directory with permissions 0755 (rwxr-xr-x)
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			// Create parent directories for file if needed.
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", err
			}
			// Create and open target file.
			outFile, err := os.Create(target)
			if err != nil {
				return "", err
			}
			// Copy file contents from the tar archive to the file.
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
		}
	}

	// Return the full path of the extracted top-level folder or file.
	return filepath.Join(dest, topLevel), nil
}

// extractZip extracts a .zip archive into the destination directory
func extractZip(src, dest string) (string, error) {
	// Open the zip archive for reading.
	r, err := zip.OpenReader(src)
	if err != nil {
		return "", err
	}
	defer r.Close()

	var topLevel string

	// Iterate over each file entry inside the zip archive.
	for _, f := range r.File {
		// Full path where this file will be extracted.
		path := filepath.Join(dest, f.Name)

		// Record the top-level folder name from first file entry.
		if topLevel == "" {
			parts := strings.Split(f.Name, "/")
			if len(parts) > 0 {
				topLevel = parts[0]
			}
		}

		// If the entry is a directory, create it with permission 0755.
		if f.FileInfo().IsDir() {
			err := os.MkdirAll(path, 0755)
			if err != nil {
				return "", err
			}
			continue
		}

		// For files, create parent directories as needed.
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}

		// Open destination file for writing with the same permissions as original.
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return "", err
		}

		// Open file inside zip for reading.
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return "", err
		}

		// Copy file contents from zip entry to destination file.
		_, err = io.Copy(outFile, rc)

		// Close both files after copy is done.
		rc.Close()
		outFile.Close()

		if err != nil {
			return "", err
		}
	}

	// Return the top-level folder extracted
	return filepath.Join(dest, topLevel), nil
}

// extract7z extracts .7z archives using the sevenzip library.
func extract7z(src, dest string) (string, error) {
	// Open the 7z archive for reading.
	r, err := sevenzip.OpenReader(src)
	if err != nil {
		return "", fmt.Errorf("failed to open 7z archive: %w", err)
	}
	defer r.Close()

	var topLevel string

	// Iterate over each file entry in the 7z archive.
	for _, f := range r.File {
		// Compute destination path.
		path := filepath.Join(dest, f.Name)

		// Capture top-level folder name from first file.
		if topLevel == "" {
			parts := strings.Split(f.Name, string(os.PathSeparator))
			if len(parts) > 0 {
				topLevel = parts[0]
			}
		}

		if f.FileInfo().IsDir() {
			// Create directory with original file permissions.
			os.MkdirAll(path, f.Mode())
			continue
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}

		// Open file inside the archive for reading.
		rc, err := f.Open()
		if err != nil {
			return "", err
		}

		// Create destination file.
		outFile, err := os.Create(path)
		if err != nil {
			rc.Close()
			return "", err
		}

		// Copy contents from archive file to destination file.
		_, err = io.Copy(outFile, rc)

		// Close both files.
		rc.Close()
		outFile.Close()

		if err != nil {
			return "", err
		}
	}

	// Return extracted top-level folder or file path.
	return filepath.Join(dest, topLevel), nil
}

// findExecutables searches a directory tree for executable files whose names
// start with the given toolName. Uses both file permission checks and the 'file' command as fallback.
func findExecutables(root string, toolName string) ([]string, error) {
	config.Debug("[DEBUG] Scanning directory for executables: %s", root)
	var executables []string

	// WalkDir walks the file tree rooted at root.
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			config.Debug("[DEBUG] WalkDir Error: %v", err)
			return err
		}
		// Skip directories as they are not executables.
		if d.IsDir() {
			return nil
		}

		// Get detailed file info for mode checks.
		info, err := d.Info()
		if err != nil {
			config.Debug("[DEBUG] Failed to get file Info for %s: %v", path, err)
			return nil // Continue walking despite error
		}
		mode := info.Mode()
		filename := filepath.Base(path)

		// Skip files that don't start with the toolName prefix.
		if !strings.HasPrefix(filename, toolName) {
			return nil
		}

		// Check if file is a regular file and has any executable bit set.
		if mode.IsRegular() && (mode.Perm()&0111 != 0 || strings.HasPrefix(mode.String(), "-rwx")) {
			config.Debug("[DEBUG] Found executable (perm): %s", path)
			executables = append(executables, path)
			return nil
		}

		// Fallback: use system 'file' command to detect executable type.
		out, err := exec.Command("file", "--brief", path).Output()
		if err != nil {
			// If 'file' command fails, ignore this file and continue.
			return nil
		}

		output := strings.ToLower(string(out))
		if strings.Contains(output, "executable") || strings.Contains(output, "mach-o") || strings.Contains(output, "elf") {
			config.Debug("[DEBUG] Found executable via file command: %s", path)
			executables = append(executables, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Return error if no executables found at all.
	if len(executables) == 0 {
		return nil, fmt.Errorf("no executables found in %s", root)
	}

	return executables, nil
}
