package binding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DialogApi adapts native desktop file and directory dialogs to simple string paths.
type DialogApi struct{ ctx context.Context }

const maxTextFileSize = 32 << 20

func NewDialogApi() *DialogApi { return &DialogApi{} }

func (a *DialogApi) startup(ctx context.Context) { a.ctx = ctx }

func (a *DialogApi) requireContext() error {
	if a.ctx == nil {
		return errors.New("native dialog is not available before application startup")
	}
	return nil
}

func (a *DialogApi) OpenFile(title string) (string, error) {
	if err := a.requireContext(); err != nil {
		return "", err
	}
	return wailsrt.OpenFileDialog(a.ctx, wailsrt.OpenDialogOptions{Title: title})
}

func (a *DialogApi) OpenDirectory(title string) (string, error) {
	if err := a.requireContext(); err != nil {
		return "", err
	}
	return wailsrt.OpenDirectoryDialog(a.ctx, wailsrt.OpenDialogOptions{Title: title})
}

func (a *DialogApi) SaveFile(title, defaultFilename string) (string, error) {
	if err := a.requireContext(); err != nil {
		return "", err
	}
	return wailsrt.SaveFileDialog(a.ctx, wailsrt.SaveDialogOptions{
		Title: title, DefaultFilename: defaultFilename, CanCreateDirectories: true,
	})
}

// ReadTextFile reads a user-selected text file for import/runner workflows.
// The size cap prevents an accidental multi-gigabyte read from freezing the UI.
func (a *DialogApi) ReadTextFile(path string) (string, error) {
	if err := a.requireContext(); err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("selected path is not a regular file")
	}
	if info.Size() > maxTextFileSize {
		return "", fmt.Errorf("text file exceeds %d MiB limit", maxTextFileSize>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTextFileSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxTextFileSize {
		return "", fmt.Errorf("text file exceeds %d MiB limit", maxTextFileSize>>20)
	}
	if !utf8.Valid(data) {
		return "", errors.New("selected file is not valid UTF-8 text")
	}
	return string(data), nil
}
