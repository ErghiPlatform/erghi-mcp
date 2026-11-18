package tools

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
)

const (
	maxKnowledgeBytes = 10 << 20
	maxLogoBytes      = 1 << 20
)

var (
	knowledgeExtensions = []string{".pdf", ".docx", ".txt", ".md", ".markdown", ".csv", ".html", ".htm"}
	logoExtensions      = []string{".png", ".jpg", ".jpeg", ".webp"}
)

// localFile reads a file the user named for an upload tool. Only document or
// image extensions are accepted, so a prompt-injected path can't send a key,
// config or dotfile into the knowledge base, where visitors could read it.
func localFile(path, field string, extensions []string, maxBytes int64) (erghi.File, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return erghi.File{}, fmt.Errorf("path must be an absolute file path, got %q", path)
	}
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(name))
	if strings.HasPrefix(name, ".") || !slices.Contains(extensions, ext) {
		return erghi.File{}, fmt.Errorf("%s: only %s files can be uploaded", name, strings.Join(extensions, ", "))
	}
	f, err := os.Open(path)
	if err != nil {
		return erghi.File{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return erghi.File{}, err
	}
	if !info.Mode().IsRegular() {
		return erghi.File{}, fmt.Errorf("%s is not a regular file", name)
	}
	if info.Size() == 0 {
		return erghi.File{}, fmt.Errorf("%s is empty", name)
	}
	if info.Size() > maxBytes {
		return erghi.File{}, fmt.Errorf("%s is %d bytes; the limit is %d", name, info.Size(), maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return erghi.File{}, err
	}
	return erghi.File{Field: field, Name: name, Content: data}, nil
}
