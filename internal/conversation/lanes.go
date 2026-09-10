package conversation

import (
	"acta2/internal/threads"
	"strings"
)

// Lane state is updated in the same ingestion transaction as its history.
// Root state also retains the lane inventory, independently of page boundaries.
func (r *Reducer) Apply(f threads.Frame) error {
	if f.Kind == "subagent/status" {
		if r.Current.Agents == nil {
			r.Current.Agents = map[string]threads.Frame{}
		}
		r.Current.Agents[str(Data(f)["lane_id"])] = f
	}
	if f.LaneID == "" {
		return r.apply(f)
	}
	if r.Current.Lanes == nil {
		r.Current.Lanes = map[string]*Current{}
	}
	c := r.Current.Lanes[f.LaneID]
	if c == nil {
		c = &Current{}
		r.Current.Lanes[f.LaneID] = c
	}
	child := Reducer{Store: laneRepository{r.Store, f.LaneID}, Current: c}
	return child.apply(f)
}

// Prefix only storage identities. Provider IDs remain intact in frame data,
// so command routing never depends on presentation keys.
type laneRepository struct {
	Repository
	lane string
}

func (r laneRepository) prefix() string { return key("lane", r.lane) + ":" }
func (r laneRepository) Get(id string) (*Item, error) {
	if strings.HasPrefix(id, r.prefix()) {
		return r.Repository.Get(id)
	}
	return r.Repository.Get(r.prefix() + id)
}
func (r laneRepository) Save(i *Item) error {
	if !strings.HasPrefix(i.ID, r.prefix()) {
		i.ID = r.prefix() + i.ID
		i.Payload["id"] = r.prefix() + str(i.Payload["id"])
	}
	i.LaneID = r.lane
	return r.Repository.Save(i)
}
func (r laneRepository) InTurn(run, turn string) ([]*Item, error) {
	items, err := r.Repository.InTurn(run, turn)
	if err != nil {
		return nil, err
	}
	out := []*Item{}
	for _, i := range items {
		if i.LaneID == r.lane {
			out = append(out, i)
		}
	}
	return out, nil
}

func (r *Reducer) subagent(f threads.Frame, d Object) error {
	id := key("agent", f.RunID, str(d["lane_id"]))
	// Claude's Agent call is replaced in place. Codex coordination calls may
	// describe several children, so their lane identities own separate cards.
	if tool := str(d["tool_id"]); tool != "" && f.Provider != "codex" {
		id = key("tool", f.RunID, tool)
	}
	i, err := r.item(f, id, "subagent")
	if err != nil {
		return err
	}
	i.Payload["kind"] = "subagent"
	i.Payload["data"] = d
	i.Payload["frame"] = frameObject(f)
	return r.save(i, f)
}

func (r *Reducer) subagentNotification(f threads.Frame, d Object) error {
	completion := str(d["completion_id"])
	if completion == "" {
		completion = str(d["tool_id"])
	}
	i, e := r.item(f, key("agent-notice", f.RunID, str(d["lane_id"]), completion), "frame")
	if e != nil {
		return e
	}
	prior := obj(obj(i.Payload["frame"])["data"])
	if i.Revision != 0 && yes(prior["context_entry"]) {
		return nil
	}
	i.Payload["frame"] = frameObject(f)
	if yes(d["context_entry"]) {
		i.Sequence = f.Sequence
		i.OutputIndex = f.OutputIndex
	}
	return r.save(i, f)
}
