package service

import (
	"errors"

	"github.com/QuantumNous/new-api/model"
)

const BillingSourceWallet = "wallet"

// FundingSource 抽象了请求预扣费使用的额度来源。
// 个人版只保留管理员钱包额度，接口仍用于隔离计费会话与额度存储。
type FundingSource interface {
	Source() string
	PreConsume(amount int) error
	Settle(delta int) error
	Refund() error
}

// ErrInsufficientWalletQuota 钱包原子预扣失败，未发生任何扣减。
var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

type WalletFunding struct {
	userId   int
	consumed int
}

func (w *WalletFunding) Source() string { return BillingSourceWallet }

func (w *WalletFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	reserved, err := model.TryReserveUserQuota(w.userId, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return ErrInsufficientWalletQuota
	}
	w.consumed = amount
	return nil
}

func (w *WalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return model.DecreaseUserQuota(w.userId, delta, false)
	}
	return model.IncreaseUserQuota(w.userId, -delta, false)
}

func (w *WalletFunding) Refund() error {
	if w.consumed <= 0 {
		return nil
	}
	// IncreaseUserQuota 是 quota += N 的非幂等操作，不能重试，否则会多退额度。
	return model.IncreaseUserQuota(w.userId, w.consumed, false)
}
