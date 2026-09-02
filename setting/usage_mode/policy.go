package usage_mode

// Mode describes how the instance is intended to be operated.
type Mode string

const ModePersonal Mode = "personal"

// Feature identifies a capability that can be enabled or disabled by the
// personal edition policy. Core relay capabilities intentionally remain
// enabled; the list below only gates platform/operations features that are not
// part of the personal edition.
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
}

// CurrentMode is kept as a compatibility API for clients that still report
// the operating mode. The personal edition has no runtime mode switch.
func CurrentMode() Mode {
	return ModePersonal
}

func IsPersonalUse() bool {
	return true
}

// IsFeatureEnabled returns whether a feature is available in the personal
// edition. Unknown features remain enabled for forward compatibility.
func IsFeatureEnabled(feature Feature) bool {
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
