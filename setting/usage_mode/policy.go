package usage_mode

import "github.com/QuantumNous/new-api/setting/operation_setting"

// Mode describes how the instance is intended to be operated.
type Mode string

const (
	ModeExternal Mode = "external"
	ModePersonal Mode = "personal"
	ModeDemo     Mode = "demo"
)

// Feature identifies a capability that can be enabled or disabled by the
// runtime usage policy. Core relay capabilities intentionally remain enabled
// in personal mode; the list below only gates platform/operations features.
type Feature string

const (
	FeatureCoreRelay           Feature = "core_relay"
	FeatureChannelManagement   Feature = "channel_management"
	FeatureModelManagement     Feature = "model_management"
	FeatureTokenManagement     Feature = "token_management"
	FeatureRequestLogs         Feature = "request_logs"
	FeatureProtocolDiagnostics Feature = "protocol_diagnostics"
	FeatureBasicAuth           Feature = "basic_auth"
	FeatureAdvancedAuth        Feature = "advanced_auth"
	FeatureMediaTasks          Feature = "media_tasks"
	FeaturePerformanceConsole  Feature = "performance_console"
	FeatureRegistration        Feature = "registration"
	FeatureEmailVerification   Feature = "email_verification"
	FeaturePasswordReset       Feature = "password_reset"
	FeatureOAuth               Feature = "oauth"
	FeatureUserManagement      Feature = "user_management"
	FeatureAffiliation         Feature = "affiliation"
	FeatureWallet              Feature = "wallet"
	FeaturePayments            Feature = "payments"
	FeatureSubscriptions       Feature = "subscriptions"
	FeatureRedemptions         Feature = "redemptions"
	FeatureCheckin             Feature = "checkin"
	FeaturePricingPortal       Feature = "pricing_portal"
	FeatureRankings            Feature = "rankings"
	FeatureTaskPlugins         Feature = "task_plugins"
	FeatureDeployments         Feature = "deployments"
	FeatureMultiNode           Feature = "multi_node"
)

var allFeatures = []Feature{
	FeatureCoreRelay,
	FeatureChannelManagement,
	FeatureModelManagement,
	FeatureTokenManagement,
	FeatureRequestLogs,
	FeatureProtocolDiagnostics,
	FeatureBasicAuth,
	FeatureAdvancedAuth,
	FeatureMediaTasks,
	FeaturePerformanceConsole,
	FeatureRegistration,
	FeatureEmailVerification,
	FeaturePasswordReset,
	FeatureOAuth,
	FeatureUserManagement,
	FeatureAffiliation,
	FeatureWallet,
	FeaturePayments,
	FeatureSubscriptions,
	FeatureRedemptions,
	FeatureCheckin,
	FeaturePricingPortal,
	FeatureRankings,
	FeatureTaskPlugins,
	FeatureDeployments,
	FeatureMultiNode,
}

var featureSet = func() map[Feature]struct{} {
	set := make(map[Feature]struct{}, len(allFeatures))
	for _, feature := range allFeatures {
		set[feature] = struct{}{}
	}
	return set
}()

var personalDisabledFeatures = map[Feature]struct{}{
	FeatureRegistration:      {},
	FeatureEmailVerification: {},
	FeaturePasswordReset:     {},
	FeatureOAuth:             {},
	FeatureUserManagement:    {},
	FeatureAffiliation:       {},
	FeatureWallet:            {},
	FeaturePayments:          {},
	FeatureSubscriptions:     {},
	FeatureRedemptions:       {},
	FeatureCheckin:           {},
	FeaturePricingPortal:     {},
	FeatureRankings:          {},
	FeatureTaskPlugins:       {},
	FeatureDeployments:       {},
	FeatureMultiNode:         {},
}

// CurrentMode resolves the persisted operation flags into one mode. Demo mode
// wins when both flags are accidentally enabled, matching the setup wizard's
// explicit mode precedence.
func CurrentMode() Mode {
	if operation_setting.DemoSiteEnabled {
		return ModeDemo
	}
	if operation_setting.SelfUseModeEnabled {
		return ModePersonal
	}
	return ModeExternal
}

func IsPersonalUse() bool {
	return CurrentMode() == ModePersonal
}

// IsFeatureEnabled returns whether a feature is allowed by the current usage
// policy. External and demo modes preserve the existing feature behavior;
// individual feature settings still decide whether a feature is configured.
func IsFeatureEnabled(feature Feature) bool {
	if !IsPersonalUse() {
		return true
	}
	if _, known := featureSet[feature]; !known {
		return true
	}
	_, disabled := personalDisabledFeatures[feature]
	return !disabled
}

// Capabilities returns a fresh map suitable for an API response. A fresh map
// prevents callers from mutating the policy's internal data.
func Capabilities() map[string]bool {
	capabilities := make(map[string]bool, len(allFeatures))
	for _, feature := range allFeatures {
		capabilities[string(feature)] = IsFeatureEnabled(feature)
	}
	return capabilities
}
