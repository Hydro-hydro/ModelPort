package usage_mode

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

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
	FeatureSystemTasks         Feature = "system_tasks"
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
	FeatureSystemTasks,
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
	FeatureAdvancedAuth:      {},
}

// Optional personal extensions are opt-in for a new installation. Keeping the
// switch in the environment makes the startup contract explicit and avoids
// silently creating or running platform-sized subsystems.
var personalOptionalFeatures = map[Feature]string{
	FeatureSystemTasks: "MODELPORT_ENABLE_SYSTEM_TASKS",
	FeatureMediaTasks:  "MODELPORT_ENABLE_MEDIA_TASKS",
	FeatureTaskPlugins: "MODELPORT_ENABLE_TASK_PLUGINS",
	FeatureDeployments: "MODELPORT_ENABLE_DEPLOYMENTS",
	FeatureMultiNode:   "MODELPORT_ENABLE_MULTI_NODE",
}

var persistedFeatureMu sync.RWMutex
var persistedOptionalFeatures = map[Feature]bool{}

// OptionalFeatures returns the optional personal-edition capabilities in a
// stable order. The values are persisted as Option records and only become
// effective on the next process start, because routing and worker registration
// are startup decisions.
func OptionalFeatures() []Feature {
	return []Feature{
		FeatureSystemTasks,
		FeatureMediaTasks,
		FeatureTaskPlugins,
		FeatureDeployments,
		FeatureMultiNode,
	}
}

func OptionalFeatureOptionKey(feature Feature) string {
	return "feature." + string(feature)
}

func OptionalFeatureOptionKeys() []string {
	features := OptionalFeatures()
	keys := make([]string, 0, len(features))
	for _, feature := range features {
		keys = append(keys, OptionalFeatureOptionKey(feature))
	}
	return keys
}

func IsOptionalFeatureOptionKey(key string) bool {
	for _, feature := range OptionalFeatures() {
		if key == OptionalFeatureOptionKey(feature) {
			return true
		}
	}
	return false
}

// SetPersistedOptionalFeatures publishes the startup configuration loaded from
// the current database. A copied map prevents callers from mutating the
// feature snapshot after publication.
func SetPersistedOptionalFeatures(features map[Feature]bool) {
	persistedFeatureMu.Lock()
	defer persistedFeatureMu.Unlock()
	persistedOptionalFeatures = make(map[Feature]bool, len(features))
	for feature, enabled := range features {
		if _, ok := personalOptionalFeatures[feature]; ok {
			persistedOptionalFeatures[feature] = enabled
		}
	}
}

func persistedOptionalFeature(feature Feature) (bool, bool) {
	persistedFeatureMu.RLock()
	enabled, ok := persistedOptionalFeatures[feature]
	persistedFeatureMu.RUnlock()
	return enabled, ok
}

func configuredOptionalFeature(feature Feature) (bool, bool) {
	if enabled, ok := persistedOptionalFeature(feature); ok {
		return enabled, true
	}
	envKey, ok := personalOptionalFeatures[feature]
	if !ok {
		return false, false
	}
	raw, present := os.LookupEnv(envKey)
	if !present {
		return false, false
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, true
	}
	return enabled, true
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
// edition. Unknown features are disabled by default so a newly added platform
// capability cannot be exposed accidentally.
func IsFeatureEnabled(feature Feature) bool {
	if _, known := featureSet[feature]; !known {
		return false
	}
	if _, disabled := personalDisabledFeatures[feature]; disabled {
		return false
	}
	// BillingOperation reconciliation is a core usage-only path and no longer
	// depends on SystemTask. The system-task tables/runner are therefore opt-in,
	// except when an asynchronous task feature needs them for polling.
	if feature == FeatureSystemTasks {
		if IsFeatureEnabled(FeatureTaskPlugins) || IsFeatureEnabled(FeatureMediaTasks) {
			return true
		}
		if enabled, configured := configuredOptionalFeature(feature); configured {
			return enabled
		}
		return false
	}
	if _, optional := personalOptionalFeatures[feature]; optional {
		enabled, configured := configuredOptionalFeature(feature)
		return configured && enabled
	}
	return true
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
