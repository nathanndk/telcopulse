package deployment

import (
	"errors"
	"testing"
	"time"
)

func validEvent() Event {
	return Event{EventID: "jenkins-event-0001", DeploymentID: "jenkins-deploy-0001", Service: "payment-service", Version: "1.4.0", CommitSHA: "abcdef0123456789abcdef0123456789abcdef01", Environment: "staging", Deployer: "ci:jenkins", Status: "Completed", OccurredAt: time.Now().UTC().Add(-time.Minute)}
}

func TestValidateDeploymentEvent(t *testing.T) {
	base := validEvent()
	if err := base.validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*Event)
	}{
		{"future time", func(e *Event) { e.OccurredAt = time.Now().Add(time.Hour) }},
		{"newline deployer", func(e *Event) { e.Deployer = "ci\noperator" }},
		{"unknown status", func(e *Event) { e.Status = "Healthy" }},
		{"non commit", func(e *Event) { e.CommitSHA = "HEAD" }},
		{"unscoped environment", func(e *Event) { e.Environment = "other" }},
		{"invalid service", func(e *Event) { e.Service = "../payment" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := base
			tt.change(&e)
			if !errors.Is(e.validate(time.Now()), ErrInvalid) {
				t.Fatalf("accepted invalid event: %+v", e)
			}
		})
	}
}

func TestDeploymentTransitions(t *testing.T) {
	allowed := map[string][]string{"Pending": {"Running", "Completed", "Failed"}, "Running": {"Completed", "Failed"}, "Completed": {"Rolled Back"}}
	statuses := []string{"Pending", "Running", "Completed", "Failed", "Rolled Back"}
	for _, from := range statuses {
		for _, to := range statuses {
			want := false
			for _, valid := range allowed[from] {
				if valid == to {
					want = true
				}
			}
			if got := nextStatus(from, to); got != want {
				t.Errorf("%s -> %s = %v, want %v", from, to, got, want)
			}
		}
	}
}
