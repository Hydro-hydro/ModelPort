package common

import "github.com/gin-gonic/gin"

// BillingSourceUsage is the personal-edition funding source. It records
// model usage without reading or modifying the legacy user wallet.
const BillingSourceUsage = "usage"

// UsageAccounting 抽象已完成预扣会话的终态用量记账操作。
// 该接口只描述普通请求结束时需要的最小调用面：读取实际预扣额度、
// 根据实际额度结算，以及异常时退款。预扣策略和追加预留仍由完整的
// BillingSettler 负责，避免把渠道切换、阶梯计费和异步任务提交耦合进终态接口。
// 由 service.BillingSession 实现，存储在 RelayInfo 上以避免循环引用。
type UsageAccounting interface {
	// GetPreConsumedQuota 返回实际预扣的额度值（信任用户可能为 0）。
	GetPreConsumedQuota() int

	// Settle 根据实际消耗额度进行结算，计算 delta = actualQuota - preConsumedQuota，
	// 同时调整资金来源（钱包/订阅）和令牌额度。
	Settle(actualQuota int) error

	// Refund 退还所有预扣费额度（资金来源 + 令牌），幂等安全。
	// 通过 gopool 异步执行。如果已经结算或退款则不做任何操作。
	Refund(c *gin.Context)
}

// BillingSettler 是完整的计费会话接口。
// UsageAccounting 是其普通终态记账的最小子集；NeedsRefund 和 Reserve
// 只在需要检查会话状态或追加预扣的调用点使用。
type BillingSettler interface {
	UsageAccounting

	// NeedsRefund 返回会话是否存在需要退还的预扣状态（未结算且未退款）。
	NeedsRefund() bool

	// Reserve 将预扣额度补到目标值；若目标值不高于当前预扣额度则不做任何事。
	Reserve(targetQuota int) error
}
