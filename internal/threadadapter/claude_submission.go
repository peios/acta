package threadadapter

import "acta2/internal/threads"

// Local slash commands may acknowledge their UUID without ever echoing the
// submitted text. Only settle a durable Acta send belonging to this exact run.
// Receipts for provider-internal commands cannot manufacture user messages.
func (s *State) claudeSubmission(p threads.ProviderFrame, id string, emit func(string, object)) {
	text, exists := s.SubmissionTexts[id]
	if id == "" || !exists || s.Submissions[id] != p.RunID || s.Messages[id].Completed {
		return
	}
	emit("message/user", object{"message_id": id, "turn_id": id, "submission_id": id,
		"state": "completed", "started_at": nil, "completed_at": p.ReceivedAt,
		"content": inputParts(text, s.SubmissionImages[id])})
	s.Messages[id] = Message{Turn: id, Kind: "userMessage", Completed: true, Submission: id}
}
