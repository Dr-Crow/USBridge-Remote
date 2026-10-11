package app

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type branchGate struct {
	Filters struct {
		Branches struct {
			Only []string `yaml:"only"`
		} `yaml:"branches"`
	} `yaml:"filters"`
}

func checkSixBranchGates(raw []byte, branch string) error {
	var cfg struct {
		Workflows map[string]struct {
			Jobs []map[string]branchGate `yaml:"jobs"`
		} `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	found := make(map[string]bool)
	for _, entry := range cfg.Workflows["agent-capability-build"].Jobs {
		for name, gate := range entry {
			if slices.Contains(gate.Filters.Branches.Only, branch) {
				found[name] = true
			}
		}
	}
	for _, name := range []string{"windows-agent", "linux-agent", "macos-agent", "web-client", "browser-lan", "lan-host"} {
		if !found[name] {
			return fmt.Errorf("missing exact branch entry for %s", name)
		}
	}
	return nil
}

func TestOwnedAgentBranchesKeepAllSixNativeAndWebGates(t *testing.T) {
	raw, err := os.ReadFile("../../../.circleci/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range []string{"agent-capability-policy", "agent-owned-http-servers", "agent-owned-websocket-workers"} {
		if err := checkSixBranchGates(raw, branch); err != nil {
			t.Fatalf("%s: %v", branch, err)
		}
	}
}

func TestCommaContainingYAMLScalarCannotStandForTwoBranches(t *testing.T) {
	for _, only := range []string{"[branch-a, branch-b]", "\n              - branch-a\n              - branch-b"} {
		raw := "workflows:\n  agent-capability-build:\n    jobs:\n"
		for _, name := range []string{"windows-agent", "linux-agent", "macos-agent", "web-client", "browser-lan", "lan-host"} {
			raw += fmt.Sprintf("      - %s:\n          filters:\n            branches:\n              only: %s\n", name, only)
		}
		if err := checkSixBranchGates([]byte(raw), "branch-b"); err != nil {
			t.Fatal(err)
		}
	}
	raw := "workflows:\n  agent-capability-build:\n    jobs:\n"
	for _, name := range []string{"windows-agent", "linux-agent", "macos-agent", "web-client", "browser-lan", "lan-host"} {
		raw += fmt.Sprintf("      - %s:\n          filters:\n            branches:\n              only: [branch-a, branch-b]\n", name)
	}
	if err := checkSixBranchGates([]byte(raw), "branch-b"); err != nil {
		t.Fatal(err)
	}
	// Corrupt only Windows; all other five jobs still admit the exact branch.
	raw = strings.Replace(raw, "only: [branch-a, branch-b]", "only:\n                - branch-a, branch-b", 1)
	if checkSixBranchGates([]byte(raw), "branch-b") == nil {
		t.Fatal("comma-containing scalar admitted branch")
	}
}
