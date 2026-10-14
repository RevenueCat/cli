package cli

import "testing"

func TestCheckpointsCommandIsExperimental(t *testing.T) {
	for _, cmd := range NewRootCmd("test").Commands() {
		if cmd.Name() == "checkpoints" {
			if !isExperimental(cmd) {
				t.Fatal("rc checkpoints should be experimental until Checkpoint rules roll out")
			}
			return
		}
	}
	t.Fatal("rc checkpoints is not registered")
}
