package common

import (
	"crypto/tls"
	//"os"
	//"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

var StartTime = time.Now().Unix() // unit: second
var Version = "v0.0.0"            // this hard coding will be replaced automatically when building, no need to manually change
// SystemName is the runtime display name for a fresh ModelPort installation.
// Upstream/project attribution strings remain unchanged elsewhere in the
// repository; this value only controls the default site name shown to users.
var SystemName = "ModelPort"
var Logo = ""

// var ChatLink = ""
// var ChatLink2 = ""
var QuotaPerUnit = 500 * 1000.0 // $0.002 / 1K tokens
// DisplayInCurrencyEnabled is an internal settings bridge; actual display is
// controlled by general_setting.quota_display_type.
var DisplayInCurrencyEnabled = true
var DisplayTokenStatEnabled = true
var DrawingEnabled = true

var DataExportEnabled = true
var DataExportInterval = 5         // unit: minute
var DataExportDefaultTime = "hour" // unit: minute
// Any options with "Secret", "Token" in its key won't be return by GetOptions

var SessionSecret = uuid.New().String()
var CryptoSecret = uuid.New().String()
var SessionCookieSecure = false
var SessionCookieTrustedURLs []string

const (
	DefaultUserSessionActiveLimit           = 50
	DefaultUserSessionIssuanceLimit         = 100
	DefaultUserSessionIssuanceWindowSeconds = 24 * 60 * 60
	DefaultUserSessionRevokedRetentionDays  = 7
	DefaultUserSessionHourlyAlertThreshold  = 5000
)

var (
	UserSessionActiveLimit           = DefaultUserSessionActiveLimit
	UserSessionIssuanceLimit         = DefaultUserSessionIssuanceLimit
	UserSessionIssuanceWindowSeconds = int64(DefaultUserSessionIssuanceWindowSeconds)
	UserSessionRevokedRetentionDays  = DefaultUserSessionRevokedRetentionDays
	UserSessionHourlyAlertThreshold  = DefaultUserSessionHourlyAlertThreshold
)

var OptionMap map[string]string
var OptionMapRWMutex sync.RWMutex

var ItemsPerPage = 10
var MaxRecentItems = 1000

var PasswordLoginEnabled = true
var PasswordLoginEncryptionEnabled = false
var TurnstileCheckEnabled = false

var EmailLoginAuthServerList = []string{
	"smtp.sendcloud.net",
	"smtp.azurecomm.net",
}

var DebugEnabled bool
var MemoryCacheEnabled bool

var LogConsumeEnabled = true

var TLSInsecureSkipVerify bool
var InsecureTLSConfig = &tls.Config{InsecureSkipVerify: true}

var SMTPServer = ""
var SMTPPort = 587
var SMTPSSLEnabled = false
var SMTPStartTLSEnabled = false
var SMTPInsecureSkipVerify = false
var SMTPForceAuthLogin = false
var SMTPAccount = ""
var SMTPFrom = ""
var SMTPToken = ""

var TurnstileSiteKey = ""
var TurnstileSecretKey = ""

var ChannelDisableThreshold = 5.0
var AutomaticDisableChannelEnabled = false
var AutomaticEnableChannelEnabled = false
var PreConsumedQuota = 500

var RetryTimes = 0

//var RootUserEmail = ""

var IsMasterNode bool

const (
	NodeNameSourceManual   = "manual"
	NodeNameSourceHostname = "hostname"
)

// NodeName 节点名称，优先从 NODE_NAME 环境变量读取，未配置时回退主机名。
// 用于审计日志和后台任务中标识节点身份；多实例部署时建议显式配置稳定 NODE_NAME。
var NodeName = ""

// NodeNameSource records how NodeName was chosen so future instance-management
// reporting can distinguish operator-configured names from automatic fallback.
var NodeNameSource = NodeNameSourceHostname

var NodeNameManuallyConfigured bool

var requestInterval int
var RequestInterval time.Duration

var SyncFrequency int // unit is second

var BatchUpdateEnabled = false
var BatchUpdateInterval int

var RelayTimeout int // unit is second

var RelayIdleConnTimeout int // unit is second

// RelayResponseHeaderTimeout limits how long the relay transport waits for the
// upstream response headers after the request has been fully written.
// 0 disables it (previous behaviour: wait forever).
//
// Note this is NOT the same as RelayTimeout (http.Client.Timeout), which covers
// the whole response read and therefore breaks legitimate long streaming calls.
// ResponseHeaderTimeout only bounds the wait for the response headers; once the
// headers arrive, streaming is unaffected.
var RelayResponseHeaderTimeout int // unit is second
var RelayMaxIdleConns int
var RelayMaxIdleConnsPerHost int

var GeminiSafetySetting string

// https://docs.cohere.com/docs/safety-modes Type; NONE/CONTEXTUAL/STRICT
var CohereSafetySetting string

const (
	RequestIdKey         = "X-Oneapi-Request-Id"
	UpstreamRequestIdKey = "X-Upstream-Request-Id"
)

const (
	RoleGuestUser  = 0
	RoleCommonUser = 1
	RoleAdminUser  = 10
	RoleRootUser   = 100
)

func IsValidateRole(role int) bool {
	return role == RoleGuestUser || role == RoleCommonUser || role == RoleAdminUser || role == RoleRootUser
}

var (
	FileUploadPermission    = RoleGuestUser
	FileDownloadPermission  = RoleGuestUser
	ImageUploadPermission   = RoleGuestUser
	ImageDownloadPermission = RoleGuestUser
)

// All duration's unit is seconds
// Shouldn't larger then RateLimitKeyExpirationDuration
var (
	GlobalApiRateLimitEnable   bool
	GlobalApiRateLimitNum      int
	GlobalApiRateLimitDuration int64

	GlobalWebRateLimitEnable   bool
	GlobalWebRateLimitNum      int
	GlobalWebRateLimitDuration int64

	CriticalRateLimitEnable   bool
	CriticalRateLimitNum            = 20
	CriticalRateLimitDuration int64 = 20 * 60

	UploadRateLimitNum            = 10
	UploadRateLimitDuration int64 = 60

	DownloadRateLimitNum            = 10
	DownloadRateLimitDuration int64 = 60

	// Per-user search rate limit (applies after authentication, keyed by user ID)
	SearchRateLimitEnable         = true
	SearchRateLimitNum            = 10
	SearchRateLimitDuration int64 = 60
)

var RateLimitKeyExpirationDuration = 20 * time.Minute

const (
	UserStatusEnabled  = 1 // don't use 0, 0 is the default value!
	UserStatusDisabled = 2 // also don't use 0
)

const (
	TokenStatusEnabled   = 1 // don't use 0, 0 is the default value!
	TokenStatusDisabled  = 2 // also don't use 0
	TokenStatusExpired   = 3
	TokenStatusExhausted = 4
)

const (
	ChannelStatusUnknown          = 0
	ChannelStatusEnabled          = 1 // don't use 0, 0 is the default value!
	ChannelStatusManuallyDisabled = 2 // also don't use 0
	ChannelStatusAutoDisabled     = 3
)
