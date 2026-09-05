package service

import relaycommon "github.com/QuantumNous/new-api/relay/common"

// BillingSourceUsage is retained as a small compatibility constant for log
// and test fixtures. BillingSession no longer models a separate funding source;
// the durable operation contains only Token, statistics, and log components.
const BillingSourceUsage = relaycommon.BillingSourceUsage
