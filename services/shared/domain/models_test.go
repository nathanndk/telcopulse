package domain

import "testing"

func TestPurchaseValidation(t *testing.T) {
	tests := []struct {
		name  string
		p     Purchase
		valid bool
	}{
		{"valid", Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "development"}, true},
		{"staging", Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "E-Wallet", Environment: "staging"}, true},
		{"linked replay", Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "development", ReplayOf: "TXN-0123456789abcdef01234567"}, true},
		{"invalid replay", Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "development", ReplayOf: "../other"}, false},
		{"missing customer", Purchase{CustomerID: "", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "development"}, false},
		{"unsupported payment", Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "Bitcoin", Environment: "development"}, false},
		{"production rejected", Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "production"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Validate() == nil; got != tt.valid {
				t.Fatalf("valid=%v want %v", got, tt.valid)
			}
		})
	}
}
func TestMaskMSISDN(t *testing.T) {
	for _, tt := range []struct{ in, want string }{{"628123450123", "62812*****123"}, {"123", "********"}, {"", "********"}} {
		t.Run(tt.in, func(t *testing.T) {
			if got := MaskMSISDN(tt.in); got != tt.want {
				t.Fatalf("got %q", got)
			}
		})
	}
}
