package threadadapter

// Automatic reviews are observations, never actionable permission requests.
// Keep their identity independent from the tool: one tool can have many reviews.
func codexApprovalReview(method string, params object, emit func(string, object)) bool {
	if method != "item/autoApprovalReview/started" && method != "item/autoApprovalReview/completed" {
		return false
	}
	review, action := obj(params["review"]), obj(params["action"])
	status := map[string]string{"inProgress": "in_progress", "approved": "approved", "denied": "denied", "timedOut": "timed_out", "aborted": "aborted"}[str(review["status"])]
	title := map[string]string{"command": "Run command", "execve": "Run program", "writeStdin": "Send input to command", "applyPatch": "Change files", "networkAccess": "Access network", "mcpToolCall": "Call tool", "requestPermissions": "Grant access"}[str(action["type"])]
	if str(params["threadId"]) == "" || str(params["turnId"]) == "" || str(params["reviewId"]) == "" || title == "" || status == "" {
		return false
	}
	completed := method == "item/autoApprovalReview/completed"
	if completed == (status == "in_progress") {
		return false
	}
	// A future decision source must be reviewed before being labelled automatic.
	if completed && params["decisionSource"] != "agent" {
		return false
	}
	for _, key := range []string{"riskLevel", "userAuthorization", "rationale"} {
		if review[key] != nil {
			if _, ok := review[key].(string); !ok {
				return false
			}
		}
	}
	if params["targetItemId"] != nil {
		if _, ok := params["targetItemId"].(string); !ok {
			return false
		}
	}
	started := stamp(params["startedAtMs"], true)
	var ended any
	if completed {
		ended = stamp(params["completedAtMs"], true)
	}
	emit("approval/review", object{"review_id": params["reviewId"], "turn_id": params["turnId"], "tool_id": nullable(params["targetItemId"]), "status": status, "title": title, "details": action, "risk_level": nullable(review["riskLevel"]), "user_authorization": nullable(review["userAuthorization"]), "rationale": nullable(review["rationale"]), "started_at": started, "completed_at": ended})
	return true
}
