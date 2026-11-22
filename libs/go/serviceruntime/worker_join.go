package serviceruntime

// Joined reports only the existing collector's positive completion signal, independently of a joined worker failure or a timed-out Wait. Stop and context cancellation never establish join.
// Joined сообщает лишь existing collector positive completion signal независимо от joined worker failure либо timed-out Wait. Stop и context cancellation не устанавливают join.
func (group *WorkerGroup) Joined() bool {
	if group == nil || group.done == nil {
		return false
	}
	select {
	case <-group.done:
		return true
	default:
		return false
	}
}
