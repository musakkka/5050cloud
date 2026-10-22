// SPDX-License-Identifier: MIT

package api

// Deployment labels, exactly as Railway shows them (docs/SPEC.md 9.1.1).
// The console's StatusGlyph, the CLI and webhooks all use these words.
const (
	LabelInitializing = "Initializing"
	LabelWaiting      = "Waiting"
	LabelBuilding     = "Building"
	LabelDeploying    = "Deploying"
	LabelActive       = "Active"
	LabelSuccess      = "Success"
	LabelCompleted    = "Completed"
	LabelCrashed      = "Crashed"
	LabelFailed       = "Failed"
	LabelRemoving     = "Removing"
	LabelRemoved      = "Removed"
	LabelSkipped      = "Skipped"
)

// StatusLabel returns the label for a deployment status. isLatestSuccess is
// true for the active deployment (SUCCESS shows "Active" only for the live
// one). CANCELLED shows "Removed" because Railway has no visible cancelled
// state; Deployment.Reason carries why. Unknown values fall back to the raw
// status so a newer server never renders an empty badge.
func StatusLabel(status DeploymentStatus, isLatestSuccess bool) string {
	switch status {
	case DepQueued:
		return LabelInitializing
	case DepWaiting:
		return LabelWaiting
	case DepBuilding:
		return LabelBuilding
	case DepDeploying:
		return LabelDeploying
	case DepSuccess:
		if isLatestSuccess {
			return LabelActive
		}
		return LabelSuccess
	case DepCompleted:
		return LabelCompleted
	case DepCrashed:
		return LabelCrashed
	case DepFailed:
		return LabelFailed
	case DepRemoving:
		return LabelRemoving
	case DepRemoved, DepCancelled:
		return LabelRemoved
	case DepSkipped:
		return LabelSkipped
	}
	return string(status)
}
