// Package faults defines the bounded synthetic failure decision contract.
package faults

// Request identifies a transaction without customer or payment details.
type Request struct {
	TransactionID string `json:"transaction_id"`
	Environment   string `json:"environment"`
}

// Decision is persisted by simulation-service before it is returned.
type Decision struct {
	Active       bool   `json:"active"`
	Scenario     string `json:"scenario"`
	DelayMS      int    `json:"delay_ms"`
	RunID        string `json:"run_id"`
	DeploymentID string `json:"deployment_id"`
	Version      string `json:"version"`
	Inject       bool   `json:"inject"`
}
