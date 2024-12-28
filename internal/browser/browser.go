// internal/browser/browser.go
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/bradsec/gofindmyfonts/internal/logging"
)

// BrowserInfo stores information about a browser
type BrowserInfo struct {
	Name    string
	Windows string
	Darwin  string
	Linux   string
}

var browsers = []BrowserInfo{
	{
		Name:    "Chrome",
		Windows: `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		Darwin:  `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`,
		Linux:   "google-chrome",
	},
	{
		Name:    "Firefox",
		Windows: `C:\Program Files\Mozilla Firefox\firefox.exe`,
		Darwin:  `/Applications/Firefox.app/Contents/MacOS/firefox`,
		Linux:   "firefox",
	},
	{
		Name:    "Safari",
		Windows: "", // Safari is not available on Windows
		Darwin:  `/Applications/Safari.app/Contents/MacOS/Safari`,
		Linux:   "", // Safari is not available on Linux
	},
	{
		Name:    "Edge",
		Windows: `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		Darwin:  `/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge`,
		Linux:   "microsoft-edge",
	},
}

// OpenBrowser attempts to open the provided URL in a browser
func OpenBrowser(url string) error {
	logging.Info(fmt.Sprintf("Attempting to open URL in browser: %s", url), "open_browser", "")

	var err error
	switch runtime.GOOS {
	case "darwin":
		// On macOS, try 'open' command first
		logging.Info("Trying macOS 'open' command", "open_browser", "")
		err = exec.Command("open", url).Start()
		if err != nil {
			logging.Error("Failed to use 'open' command", "open_browser", "", err)
			return tryAlternativeBrowsers(url)
		}
	case "windows":
		logging.Info("Trying Windows 'start' command", "open_browser", "")
		err = exec.Command("cmd", "/c", "start", url).Start()
		if err != nil {
			logging.Error("Failed to use 'start' command", "open_browser", "", err)
			return tryAlternativeBrowsers(url)
		}
	case "linux":
		logging.Info("Trying Linux 'xdg-open' command", "open_browser", "")
		err = exec.Command("xdg-open", url).Start()
		if err != nil {
			logging.Error("Failed to use 'xdg-open' command", "open_browser", "", err)
			return tryAlternativeBrowsers(url)
		}
	default:
		errMsg := fmt.Sprintf("Unsupported operating system: %s", runtime.GOOS)
		logging.Error(errMsg, "open_browser", "", fmt.Errorf(errMsg))
		return fmt.Errorf(errMsg)
	}

	return nil
}

// tryAlternativeBrowsers attempts to open the URL using installed browsers
func tryAlternativeBrowsers(url string) error {
	logging.Info("Attempting to find alternative browsers", "try_browser", "")

	for _, browser := range browsers {
		path := ""
		switch runtime.GOOS {
		case "darwin":
			path = browser.Darwin
		case "windows":
			// Try both Program Files paths for Windows
			if browser.Name == "Chrome" {
				// Try x64 path first
				chromePath := `C:\Program Files\Google\Chrome\Application\chrome.exe`
				if _, err := os.Stat(chromePath); err == nil {
					path = chromePath
				} else {
					// Fallback to x86 path
					path = `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`
				}
			} else {
				path = browser.Windows
			}
		case "linux":
			path = browser.Linux
		}

		if path != "" {
			logging.Info(fmt.Sprintf("Trying browser: %s", browser.Name), "try_browser", path)
			if _, err := os.Stat(path); err == nil {
				if err := exec.Command(path, url).Start(); err != nil {
					logging.Error(fmt.Sprintf("Failed to start %s", browser.Name), "try_browser", path, err)
				} else {
					logging.Info(fmt.Sprintf("Successfully opened URL with %s", browser.Name), "try_browser", path)
					return nil
				}
			} else {
				logging.Info(fmt.Sprintf("Browser not found: %s", browser.Name), "try_browser", path)
			}
		}
	}

	errMsg := "No suitable browser found"
	logging.Error(errMsg, "try_browser", "", fmt.Errorf(errMsg))
	return fmt.Errorf(errMsg)
}
