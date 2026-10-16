//go:build unit

package handler

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Exercise the real signature verifier, confirmation query, service, and HTTP
// handler against an isolated in-memory database and a loopback EasyPay server.
func TestEasyPayWebhookConfirmationFailureDoesNotFulfill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		status int
		money  string
	}{
		{"non_finite_amount", http.StatusOK, `"NaN"`},
		{"http_failure_with_paid_body", http.StatusServiceUnavailable, `"10.00"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite", "file:webhook_confirmation_"+tc.name+"?mode=memory&cache=shared")
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
			t.Cleanup(func() { _ = client.Close() })
			user, err := client.User.Create().SetEmail("confirmation@example.test").
				SetPasswordHash("test-only").SetUsername("confirmation").SetBalance(7).Save(ctx)
			require.NoError(t, err)
			order, err := client.PaymentOrder.Create().SetUserID(user.ID).
				SetUserEmail(user.Email).SetUserName(user.Username).
				SetAmount(10).SetPayAmount(10).SetFeeRate(0).
				SetRechargeCode("TEST-CONFIRM").SetOutTradeNo("confirmation-order").
				SetPaymentType(payment.TypeAlipay).SetOrderType(payment.OrderTypeBalance).
				SetStatus(service.OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).
				SetClientIP("127.0.0.1").SetSrcHost("example.test").Save(ctx)
			require.NoError(t, err)

			var queries atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries.Add(1)
				if r.URL.Path != "/api.php" || r.URL.Query().Get("act") != "order" {
					t.Errorf("unexpected confirmation request: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"code":1,"status":1,"pid":"1001","out_trade_no":"confirmation-order","trade_no":"trade-1","money":%s}`, tc.money)
			}))
			t.Cleanup(upstream.Close)
			const testKey = "local-test-merchant-key"
			prov, err := provider.NewEasyPay("test", map[string]string{
				"pid": "1001", "pkey": testKey, "apiBase": upstream.URL,
				"notifyUrl": "https://example.test/notify", "returnUrl": "https://example.test/return",
			})
			require.NoError(t, err)
			registry := payment.NewRegistry()
			registry.Register(prov)
			svc := service.NewPaymentService(client, registry, nil, nil, nil, nil, nil, nil, nil)
			h := NewPaymentWebhookHandler(svc, registry)
			router := gin.New()
			router.POST("/notify", h.EasyPayNotify)

			// EasyPay signs nonempty fields in lexical key order without URL encoding.
			signed := "money=10.00&out_trade_no=confirmation-order&pid=1001&trade_no=trade-1&trade_status=TRADE_SUCCESS&type=alipay"
			params, err := url.ParseQuery(signed)
			require.NoError(t, err)
			params.Set("sign", fmt.Sprintf("%x", md5.Sum([]byte(signed+testKey))))
			params.Set("sign_type", "MD5")
			req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(params.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, http.StatusBadGateway, response.Code)
			require.Equal(t, "confirm failed", response.Body.String())
			require.Equal(t, int32(1), queries.Load(), "confirmation must actually reach the mock upstream")
			afterOrder, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, service.OrderStatusPending, afterOrder.Status)
			require.Equal(t, order.PaymentTradeNo, afterOrder.PaymentTradeNo)
			afterUser, err := client.User.Get(ctx, user.ID)
			require.NoError(t, err)
			require.Equal(t, user.Balance, afterUser.Balance)
			audits, err := client.PaymentAuditLog.Query().All(ctx)
			require.NoError(t, err)
			require.Len(t, audits, 1)
			require.Equal(t, "PAYMENT_UPSTREAM_UNCONFIRMED", audits[0].Action)
		})
	}
}
