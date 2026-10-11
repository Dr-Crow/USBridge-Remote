package app

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// This experimental merge must execute the complete preview gates while
// publishing only their source-free receipts, including on the new branch.
func TestPreviewOwnerWorkflowReceiptsOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", ".circleci", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	jobs := config["jobs"].(map[string]any)
	workflow := config["workflows"].(map[string]any)["agent-capability-build"].(map[string]any)
	for _, branch := range []string{"source-preview-experiment", "source-preview-owned-engine"} {
		var admitted, artifacts, runs []string
		for _, item := range workflow["jobs"].([]any) {
			for name, raw := range item.(map[string]any) {
				entry := raw.(map[string]any)
				only := entry["filters"].(map[string]any)["branches"].(map[string]any)["only"].([]any)
				selected := false
				for _, b := range only {
					if b == branch {
						selected = true
					}
				}
				if !selected {
					continue
				}
				admitted = append(admitted, name)
				var walk func([]any)
				walk = func(steps []any) {
					for _, step := range steps {
						m, ok := step.(map[string]any)
						if !ok {
							continue
						}
						for key, raw := range m {
							switch key {
							case "when", "unless":
								v := raw.(map[string]any)
								active := previewOwnerCondition(t, v["condition"], branch)
								if key == "unless" {
									active = !active
								}
								if active {
									walk(v["steps"].([]any))
								}
							case "store_artifacts":
								artifacts = append(artifacts, raw.(map[string]any)["path"].(string))
							case "run":
								if v, ok := raw.(map[string]any); ok {
									if command, ok := v["command"].(string); ok {
										runs = append(runs, command)
									}
								}
							}
						}
					}
				}
				walk(jobs[name].(map[string]any)["steps"].([]any))
			}
		}
		sort.Strings(admitted)
		sort.Strings(artifacts)
		if !reflect.DeepEqual(admitted, []string{"linux-agent", "windows-preview-process", "windows-source-preview"}) {
			t.Fatalf("%s jobs: %v", branch, admitted)
		}
		if !reflect.DeepEqual(artifacts, []string{"artifacts/preview-evidence", "artifacts/windows-preview-evidence", "artifacts/windows-preview-process-evidence"}) {
			t.Fatalf("%s artifact scope: %v", branch, artifacts)
		}
		for _, required := range []string{"bash .circleci/test-source-preview.sh", "bash .circleci/test-source-preview-dialog.sh", "bash .circleci/test-source-preview-engine.sh"} {
			found := false
			for _, run := range runs {
				if run == required {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s missing %s", branch, required)
			}
		}
	}
}

func previewOwnerCondition(t *testing.T, raw any, branch string) bool {
	t.Helper()
	m := raw.(map[string]any)
	if len(m) != 1 {
		t.Fatal("ambiguous branch condition")
	}
	if raw, ok := m["or"]; ok {
		result := false
		for _, v := range raw.([]any) {
			result = previewOwnerCondition(t, v, branch) || result
		}
		return result
	}
	if raw, ok := m["equal"]; ok {
		v := raw.([]any)
		if len(v) != 2 || v[1] != "<< pipeline.git.branch >>" {
			t.Fatal("unexpected branch equality")
		}
		return v[0] == branch
	}
	t.Fatal("unsupported branch condition")
	return false
}

func TestPreviewCloseAccountingBranchRunsOnlyNativeOwnerGate(t *testing.T) {
	raw, err := os.ReadFile("../../../.circleci/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	workflow := cfg["workflows"].(map[string]any)["agent-capability-build"].(map[string]any)
	var selected []string
	for _, raw := range workflow["jobs"].([]any) {
		for name, v := range raw.(map[string]any) {
			only := v.(map[string]any)["filters"].(map[string]any)["branches"].(map[string]any)["only"].([]any)
			for _, b := range only {
				if b == "preview-owner-close-accounting" {
					selected = append(selected, name)
				}
			}
		}
	}
	if !reflect.DeepEqual(selected, []string{"windows-preview-process"}) {
		t.Fatal("unexpected fast ownership workflow", selected)
	}
	job := cfg["jobs"].(map[string]any)[selected[0]].(map[string]any)
	var artifacts []string
	for _, raw := range job["steps"].([]any) {
		if step, ok := raw.(map[string]any); ok {
			if a, ok := step["store_artifacts"]; ok {
				artifacts = append(artifacts, a.(map[string]any)["path"].(string))
			}
		}
	}
	if !reflect.DeepEqual(artifacts, []string{"artifacts/windows-preview-process-evidence"}) {
		t.Fatal("fast owner publication scope changed", artifacts)
	}
}
