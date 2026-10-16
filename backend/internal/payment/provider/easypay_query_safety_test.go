package provider

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestEasyPayConfirmRejectsNonFiniteAmounts(t *testing.T) {
	for _, tc := range []struct {
		name, money string
		notified    float64
	}{
		{"query_nan", "NaN", 10}, {"query_positive_infinity", "+Inf", 10}, {"query_negative_infinity", "-Inf", 10},
		{"query_zero", "0", 0}, {"query_negative", "-1", -1}, {"query_invalid", "invalid", 10},
		{"notify_nan", "10.00", math.NaN()}, {"notify_positive_infinity", "10.00", math.Inf(1)},
		{"both_positive_infinity", "+Inf", math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"code":1,"trade_no":"2026101600001","money":"` + tc.money + `","status":1}`
			provider, _ := newConfirmTestEasyPay(t, body, http.StatusOK)
			n := confirmTestNotification()
			n.Amount = tc.notified
			if err := provider.ConfirmNotification(context.Background(), n); err == nil {
				t.Fatal("non-finite amount must not confirm payment")
			}
		})
	}
}

func TestEasyPayQueryRejectsHTTPFailure(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusBadGateway} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			body := `{"code":1,"trade_no":"2026101600001","money":"10.00","status":1}`
			provider, _ := newConfirmTestEasyPay(t, body, status)
			for _, confirm := range []bool{false, true} {
				var err error
				if confirm {
					err = provider.ConfirmNotification(context.Background(), confirmTestNotification())
				} else {
					_, err = provider.QueryOrder(context.Background(), "sub2_ORDER1")
				}
				if err == nil {
					t.Fatalf("HTTP %d with paid-looking JSON must fail (confirm=%v)", status, confirm)
				}
				if !strings.Contains(err.Error(), strconv.Itoa(status)) {
					t.Fatalf("error must retain HTTP status: %v", err)
				}
				if strings.Contains(err.Error(), "pkey-1") || strings.Contains(err.Error(), "key=") {
					t.Fatalf("error leaked merchant credentials: %v", err)
				}
			}
		})
	}
}

func TestEasyPayConfirmPreservesAmountTolerance(t *testing.T) {
	for _, tc := range []struct {
		name, money string
		wantErr     bool
	}{
		{"normal_string", `"10.00"`, false}, {"normal_number", `10`, false},
		{"within_tolerance_above", `"10.004"`, false}, {"within_tolerance_below", `"9.996"`, false},
		{"outside_tolerance_above", `"10.006"`, true}, {"outside_tolerance_below", `"9.994"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"code":1,"trade_no":"2026101600001","money":` + tc.money + `,"status":1}`
			provider, _ := newConfirmTestEasyPay(t, body, http.StatusOK)
			err := provider.ConfirmNotification(context.Background(), confirmTestNotification())
			if (err != nil) != tc.wantErr {
				t.Fatalf("ConfirmNotification error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
