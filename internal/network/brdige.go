// Bridge bütün container-lərin host-da qoşulduğu virtual switch-dir.
// Docker-də bunun adı `docker0`, bizdə `azcontainer0`.
package network

import (
	"fmt"
	"os/exec"
	"strings"
)

const BridgeName = "azcontainer0"

// EnsureBridge — bridge mövcuddursa heç nə etmir, yoxdursa yaradır.
//
// Bridge bir dəfə yaradılır, sonra bütün container-lər ona qoşulur.
// Host reboot-dan sonra itir, amma birinci `run`-da yenidən yaradılır.
func EnsureBridge() error {
	// Yoxla: bridge mövcuddurmu?
	if bridgeExists() {
		return nil
	}

	fmt.Printf("[host] Bridge yaradılır: %s\n", BridgeName)

	// 1. Bridge yarat.
	if err := runIP("link", "add", "name", BridgeName, "type", "bridge"); err != nil {
		return fmt.Errorf("bridge yarat: %w", err)
	}

	// 2. Bridge-ə IP ver.
	if err := runIP("addr", "add", BridgeCIDR, "dev", BridgeName); err != nil {
		return fmt.Errorf("bridge IP: %w", err)
	}

	// 3. Bridge-i aktivləşdir (UP).
	if err := runIP("link", "set", BridgeName, "up"); err != nil {
		return fmt.Errorf("bridge UP: %w", err)
	}

	return nil
}

// bridgeExists — bridge mövcuddurmu?
func bridgeExists() bool {
	out, err := exec.Command("ip", "link", "show", BridgeName).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), BridgeName)
}

// runIP — `ip` komandasını çağırır və error-da output-u qaytarır.
func runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
