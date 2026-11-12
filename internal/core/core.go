package core

import "errors"

type TaskStatus string
type TenantStatus string

const (
	Accepted   TaskStatus = "accepted"
	InProgress TaskStatus = "in_progress"
	Done       TaskStatus = "done"
	FailedTask TaskStatus = "failed"
)

var ErrInvalidTransition = errors.New("invalid state transition")

// NextTenantStatus validates a terminal result against the existing tenant state.
func NextTenantStatus(current TenantStatus, typ string, outcome TaskStatus) (TenantStatus, error) {
	if outcome != Done && outcome != FailedTask {
		return "", ErrInvalidTransition
	}
	switch typ {
	case "deploy":
		if current != "provisioning" {
			return "", ErrInvalidTransition
		}
		if outcome == Done {
			return "active", nil
		}
	case "update":
		if current != "updating" {
			return "", ErrInvalidTransition
		}
		if outcome == Done {
			return "active", nil
		}
	case "destroy":
		if current != "destroying" {
			return "", ErrInvalidTransition
		}
		if outcome == Done {
			return "destroyed", nil
		}
	default:
		return "", ErrInvalidTransition
	}
	return "failed", nil
}

// AcceptProgress is monotonic: a terminal update can bypass in_progress if delivery is out of order.
func AcceptProgress(current, incoming TaskStatus) bool {
	if current == Done || current == FailedTask {
		return false
	}
	if incoming == InProgress {
		return current == Accepted
	}
	if incoming == Done || incoming == FailedTask {
		return current == Accepted || current == InProgress
	}
	return false
}

func IsTerminal(s TaskStatus) bool { return s == Done || s == FailedTask }
