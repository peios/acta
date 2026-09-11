package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func (s *State) claudeHeartbeat(p threads.ProviderFrame, m object, emit func(string, object)) bool {
	id := str(m["parent_tool_use_id"])
	t := s.Tools[id]
	n, ok := m["elapsed_time_seconds"].(json.Number)
	seconds, err := n.Float64()
	suffix, match := strings.CutPrefix(str(m["tool_use_id"]), id+"-heartbeat-")
	_, indexErr := strconv.ParseUint(suffix, 10, 64)
	if t == nil || id == "" || t.Name != str(m["tool_name"]) || !ok || err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > float64(math.MaxInt64/int64(time.Second)) || !match || indexErr != nil {
		return false
	}
	// A delayed heartbeat is not permission to resurrect a finished tool.
	if terminalTool(t.Status) {
		return true
	}
	changed := false
	if t.Started == nil {
		t.Started = p.ReceivedAt.Add(-time.Duration(seconds * float64(time.Second)))
		changed = true
	}
	if t.Status == "pending" || t.Status == "preparing" {
		t.Status = "running"
		changed = true
	}
	if changed {
		emitTool(t, emit)
	}
	return true
}
