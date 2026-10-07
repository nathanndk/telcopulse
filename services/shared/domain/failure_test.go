package domain

import "testing"

func TestFailureClass(t *testing.T) {
	cases := []struct{ status, code, want string }{
		{"SUCCESS", "", FailureNone},
		{"FAILED", "INSUFFICIENT_BALANCE", FailureBusiness},
		{"FAILED", "DB_TIMEOUT", FailureTechnical},
		{"FAILED", "SIMULATED_PAYMENT_DECLINED", FailureTechnical},
		{"FAILED", "BAD_DEPLOYMENT_PAYMENT_FAILURE", FailureTechnical},
		{"FAILED", "PACKAGE_UNAVAILABLE", FailureTechnical},
		{"FAILED", "", FailureTechnical},
	}
	for _, c := range cases {
		if got := FailureClass(c.status, c.code); got != c.want {
			t.Errorf("FailureClass(%q,%q)=%q want %q", c.status, c.code, got, c.want)
		}
	}
}
