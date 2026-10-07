package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/revenuecat/cli/internal/api"
)

func showExperimentUpdateChanges(rt *Runtime, changes api.ExperimentUpdate) {
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var value any
		_ = json.Unmarshal(changes[key], &value)
		rt.Out.Field("Change "+strings.ReplaceAll(key, "_", " "), experimentChangeValue(value))
	}
}

func experimentChangeValue(value any) string {
	switch value := value.(type) {
	case nil:
		return "clear"
	case string:
		return value
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			parts[i] = experimentChangeValue(item)
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, strings.ReplaceAll(key, "_", " ")+": "+experimentChangeValue(value[key]))
		}
		return strings.Join(parts, " · ")
	default:
		return fmt.Sprint(value)
	}
}
