// Package version espone la versione del binario, iniettata in fase di build con
// -ldflags "-X github.com/toigoweb/totem/internal/version.Version=1.2.3".
package version

// Version è "dev" nelle build locali.
var Version = "dev"
