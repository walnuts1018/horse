package purchase

import (
	"context"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type purchaseItemAccountingPlan struct {
	payments             []Payment
	replacePayments      bool
	syncPostedAccounting bool
}

func buildPurchaseItemAccountingPlan(status string, payments []Payment, replacePayments, allocationsChanged, deferAccountingSync bool) purchaseItemAccountingPlan {
	return purchaseItemAccountingPlan{
		payments: payments, replacePayments: replacePayments,
		syncPostedAccounting: status == postedStatus && allocationsChanged && !deferAccountingSync,
	}
}

func adjustPaymentsToTotal(payments []Payment, total int64) ([]Payment, error) {
	weights := make([]domain.Weight, 0, len(payments))
	for _, payment := range payments {
		weights = append(weights, domain.Weight{PoolID: payment.AccountID, Value: payment.AmountYen})
	}
	amounts, err := domain.Allocate(total, weights)
	if err != nil {
		return nil, err
	}
	amountByAccount := make(map[string]int64, len(amounts))
	for _, amount := range amounts {
		amountByAccount[amount.PoolID] = amount.Amount
	}
	adjusted := make([]Payment, 0, len(payments))
	for _, payment := range payments {
		if amount := amountByAccount[payment.AccountID]; amount > 0 {
			adjusted = append(adjusted, Payment{AccountID: payment.AccountID, AmountYen: amount})
		}
	}
	return adjusted, nil
}

func applyPurchaseItemPaymentPlan(ctx context.Context, tx pgx.Tx, executor bob.Executor, householdID gofrsuuid.UUID, purchase *models.Purchase, plan purchaseItemAccountingPlan) error {
	if !plan.replacePayments {
		return nil
	}
	validated, err := validatePayments(ctx, tx, householdID, plan.payments)
	if err != nil {
		return err
	}
	return replaceUpdatePayments(ctx, executor, purchase, validated)
}

func applyPurchaseItemAccountingSync(ctx context.Context, executor bob.Executor, purchase *models.Purchase, plan purchaseItemAccountingPlan) error {
	if !plan.syncPostedAccounting {
		return nil
	}
	return syncPostedPurchaseAccounting(ctx, executor, purchase)
}
