//go:build !darwin

package inject

import "os"

func defaultPasteDriver() PasteDriver {
	if driver, ok := defaultBrokerPasteDriver(
		os.Getenv("TALKA_PASTE_BROKER_SOCKET"),
		os.Getenv("TALKA_PASTE_BROKER_URL"),
	); ok {
		return driver
	}
	return appleScriptPasteDriver{}
}
