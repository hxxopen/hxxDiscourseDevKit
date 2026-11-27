// Package task runs cancellable background operations and publishes progress.
package task

import (
	"time"
)

// Status is the lifecycle state of a background task.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Terminal reports whether the task has finished.
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

// Task is the public view of a background operation.
type Task struct {
	TaskID       string     `json:"task_id"`
	Type         string     `json:"type"`
	WorkspaceID  string     `json:"workspace_id,omitempty"`
	Status       Status     `json:"status"`
	Progress     int        `json:"progress"`
	Message      string     `json:"message"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Cancelable   bool       `json:"cancelable"`
}

// Event is published to SSE subscribers.
type Event struct {
	Type string `json:"type"`
	Task Task   `json:"task"`
}
