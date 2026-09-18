//go:build windows

package tui

import "golang.org/x/sys/windows"

// openURLPlatform opens url with ShellExecuteW, which receives it as a lone
// lpFile parameter. Don't switch this back to `cmd /c start`: cmd.exe
// re-parses its whole command line and mangles URLs containing characters it
// treats specially.
func openURLPlatform(url string) error {
	u, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}
