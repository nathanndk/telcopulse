// Package domain defines TelcoPulse's vendor-independent transaction contracts.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Customer is a synthetic subscriber. Only masked numbers leave the repository.
type Customer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	MSISDN  string `json:"msisdn_masked"`
	Balance int64  `json:"balance_idr"`
}

// Package is an available data entitlement, priced in whole Indonesian rupiah.
type Package struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	DataGB int    `json:"data_gb"`
	Days   int    `json:"days"`
	Price  int64  `json:"price_idr"`
}

// Purchase requests a synthetic purchase. No real payment provider is contacted.
type Purchase struct {
	CustomerID    string `json:"customer_id"`
	PackageID     string `json:"package_id"`
	PaymentMethod string `json:"payment_method"`
	Environment   string `json:"environment"`
	ReplayOf      string `json:"replay_of,omitempty"`
}

var transactionIDPattern = regexp.MustCompile(`^TXN-[a-f0-9]{24}$`)

// Validate rejects unsupported purchase inputs before database work.
func (p Purchase) Validate() error {
	if p.CustomerID == "" || p.PackageID == "" || len(p.CustomerID) > 80 || len(p.PackageID) > 80 {
		return errors.New("select a valid customer and package")
	}
	if !slices.Contains([]string{"Pulsa", "E-Wallet", "Credit Card", "Virtual Account"}, p.PaymentMethod) {
		return errors.New("unsupported payment method")
	}
	if !slices.Contains([]string{"development", "staging"}, p.Environment) {
		return errors.New("synthetic transactions are restricted to development and staging")
	}
	if p.ReplayOf != "" && !transactionIDPattern.MatchString(p.ReplayOf) {
		return errors.New("select a valid failed transaction to replay")
	}
	return nil
}

// Step is persisted evidence for a stage of the purchase workflow.
type Step struct {
	Service    string  `json:"service"`
	Operation  string  `json:"operation"`
	Status     string  `json:"status"`
	DurationMS float64 `json:"duration_ms"`
	SpanID     string  `json:"span_id"`
}

// Transaction holds the immutable result and correlated evidence of a purchase.
type Transaction struct {
	ID            string    `json:"id"`
	ReplayOf      string    `json:"replay_of,omitempty"`
	TraceID       string    `json:"trace_id"`
	CustomerID    string    `json:"customer_id"`
	CustomerName  string    `json:"customer_name"`
	MSISDN        string    `json:"msisdn_masked"`
	PackageID     string    `json:"package_id"`
	PackageName   string    `json:"package_name"`
	PaymentMethod string    `json:"payment_method"`
	Environment   string    `json:"environment"`
	Status        string    `json:"status"`
	ErrorCode     string    `json:"error_code"`
	Amount        int64     `json:"amount_idr"`
	DurationMS    float64   `json:"duration_ms"`
	CreatedAt     time.Time `json:"created_at"`
	Steps         []Step    `json:"steps"`
}

// TransactionPage is a bounded page of matching transactions.
type TransactionPage struct {
	Items    []Transaction `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// Point represents an observed minute of transaction activity.
type Point struct {
	Time        time.Time `json:"time"`
	Total       int       `json:"total"`
	SuccessRate float64   `json:"success_rate"`
	P95MS       float64   `json:"p95_ms"`
}

// Overview contains computed statistics, never demo chart constants.
type Overview struct {
	Total       int         `json:"total"`
	Success     int         `json:"success"`
	Failed      int         `json:"failed"`
	SuccessRate float64     `json:"success_rate"`
	P95MS       float64     `json:"p95_ms"`
	PerMinute   int         `json:"per_minute"`
	Series      []Point     `json:"series"`
	PurchaseSLO PurchaseSLO `json:"purchase_slo"`
}

// NewID generates cryptographically random correlation identifiers.
func NewID(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// MaskMSISDN leaves only the prefix and last three digits visible.
func MaskMSISDN(s string) string {
	if len(s) < 8 {
		return "********"
	}
	return s[:5] + "*****" + s[len(s)-3:]
}
