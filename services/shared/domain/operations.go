package domain

// Operation carries stable identity and correlation across internal domain calls.
type Operation struct {
	TransactionID string `json:"transaction_id"`
	TraceID       string `json:"trace_id"`
	CustomerID    string `json:"customer_id"`
	PackageID     string `json:"package_id"`
	Environment   string `json:"environment"`
	PaymentMethod string `json:"payment_method"`
	Amount        int64  `json:"amount_idr"`
	Days          int    `json:"days"`
	Outcome       string `json:"outcome"`
}

// OperationResult is a durable idempotent domain outcome.
type OperationResult struct {
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
}

// ValidOperation validates required correlation and purchase context at service boundaries.
func ValidOperation(op Operation) bool {
	return len(op.TransactionID) >= 16 && len(op.TransactionID) <= 100 && len(op.TraceID) == 32 && op.CustomerID != "" && len(op.CustomerID) <= 80 && op.PackageID != "" && len(op.PackageID) <= 80 && op.Amount > 0 && op.Days > 0 && op.Days <= 366 && (Purchase{CustomerID: op.CustomerID, PackageID: op.PackageID, PaymentMethod: op.PaymentMethod, Environment: op.Environment}).Validate() == nil
}
