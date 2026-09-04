package service

import (
	"errors"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	BillingSourceWallet = "wallet"
	BillingSourceUsage  = relaycommon.BillingSourceUsage
)

// FundingSource 抽象了请求预扣费使用的额度来源。
// 个人版只记录模型用量，不读写管理员钱包；WalletFunding 保留用于兼容
// 已存在的历史结算数据。
type FundingSource interface {
	Source() string
	PreConsume(amount int) error
	Settle(delta int) error
	Refund() error
}

// UsageFunding 表示个人版的用量记账来源。
// 用户余额不参与请求授权或计费结算，实际用量由调用方写入 used_quota
// 和消费日志；Token 额度仍由 BillingSession 单独处理。
type UsageFunding struct{}

func (*UsageFunding) Source() string       { return BillingSourceUsage }
func (*UsageFunding) PreConsume(int) error { return nil }
func (*UsageFunding) Settle(int) error     { return nil }
func (*UsageFunding) Refund() error        { return nil }

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
