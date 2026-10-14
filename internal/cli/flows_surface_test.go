package cli

import "testing"

func TestFlowsCommandIsExperimental(t *testing.T) {
	for _, cmd := range NewRootCmd("test").Commands() {
		if cmd.Name() == "flows" {
			if !isExperimental(cmd) {
				t.Fatal("rc flows should be experimental until Checkpoint rules roll out")
			}
			return
		}
	}
	t.Fatal("rc flows is not registered")
}
