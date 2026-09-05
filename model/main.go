package model

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/usage_mode"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var commonGroupCol string
var commonKeyCol string
var commonTrueVal string
var commonFalseVal string

var logKeyCol string
var logGroupCol string

// jsonScanBytes 归一化 json 列的驱动返回值:不同驱动/协议模式下同一列可能
// 以 []byte 或 string 返回,静默丢弃 string 会导致字段被清零而不报错。
func jsonScanBytes(value interface{}) []byte {
	switch v := value.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}

func initCol() {
	// init common column names
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		commonGroupCol = `"group"`
		commonKeyCol = `"key"`
		commonTrueVal = "true"
		commonFalseVal = "false"
	} else {
		commonGroupCol = "`group`"
		commonKeyCol = "`key`"
		commonTrueVal = "1"
		commonFalseVal = "0"
	}
	switch common.LogDatabaseType() {
	case common.DatabaseTypePostgreSQL:
		logGroupCol = `"group"`
		logKeyCol = `"key"`
	default:
		logGroupCol = "`group`"
		logKeyCol = "`key`"
	}
}

var DB *gorm.DB

var LOG_DB *gorm.DB

func CheckSetup() {
	setup := GetSetup()
	if setup == nil {
		// A missing setup row is valid only for an empty database. Do not create
		// one from an existing root account: that would silently repair a legacy
		// database instead of enforcing the fresh-install contract.
		if RootUserExists() {
			common.SysLog("setup record is missing while a root user exists; a fresh data directory is required")
		} else {
			common.SysLog("system is not initialized and no root user exists")
		}
		constant.Setup = false
	} else {
		// Setup record exists, system is initialized
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.Setup = true
	}
}

func isClickHouseDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "clickhouse://") ||
		strings.HasPrefix(dsn, "tcp://") ||
		strings.HasPrefix(dsn, "http://") ||
		strings.HasPrefix(dsn, "https://")
}

func normalizeClickHouseDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme != "https" {
		return dsn
	}
	query := parsed.Query()
	if _, ok := query["secure"]; !ok {
		query.Set("secure", "true")
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

func chooseDB(envName string, isLog bool) (*gorm.DB, common.DatabaseType, error) {
	dsn := os.Getenv(envName)
	if dsn != "" {
		if isClickHouseDSN(dsn) {
			if !isLog {
				return nil, "", fmt.Errorf("%s does not support ClickHouse; use SQLite, MySQL, or PostgreSQL for the primary database and LOG_SQL_DSN for ClickHouse logs", envName)
			}
			common.SysLog("using ClickHouse as log database")
			db, err := gorm.Open(clickhouse.Open(normalizeClickHouseDSN(dsn)), newGormConfig(false))
			return db, common.DatabaseTypeClickHouse, err
		}
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			// Use PostgreSQL
			common.SysLog("using PostgreSQL as database")
			// 同时关闭 pgx 隐式与 GORM 显式预处理语句:命名 prepared statement 与
			// 事务池代理(PgBouncer/Neon/Supabase)不兼容,会触发 FATAL 08P01/42P05。
			db, err := gorm.Open(postgres.New(postgres.Config{
				DSN:                  dsn,
				PreferSimpleProtocol: true,
			}), newGormConfig(false))
			return db, common.DatabaseTypePostgreSQL, err
		}
		if strings.HasPrefix(dsn, "local") {
			common.SysLog("SQL_DSN not set, using SQLite as database")
			db, err := gorm.Open(sqlite.Open(common.SQLitePath), newGormConfig(true))
			return db, common.DatabaseTypeSQLite, err
		}
		// Use MySQL
		common.SysLog("using MySQL as database")
		// check parseTime
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		db, err := gorm.Open(mysql.Open(dsn), newGormConfig(true))
		return db, common.DatabaseTypeMySQL, err
	}
	// Use SQLite
	common.SysLog("SQL_DSN not set, using SQLite as database")
	db, err := gorm.Open(sqlite.Open(common.SQLitePath), newGormConfig(true))
	return db, common.DatabaseTypeSQLite, err
}

func InitDB() (err error) {
	db, dbType, err := chooseDB("SQL_DSN", false)
	if err == nil {
		common.SetMainDatabaseType(dbType)
		if os.Getenv("LOG_SQL_DSN") == "" {
			common.SetLogDatabaseType(dbType)
		}
		initCol()
		if common.DebugEnabled {
			db = db.Debug()
		}
		DB = db
		// MySQL charset/collation startup check: ensure Chinese-capable charset
		if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
			if err := checkMySQLChineseSupport(DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		if err := loadPersistedOptionalFeatureSettings(); err != nil {
			return err
		}
		if !common.IsMasterNode {
			return nil
		}
		common.SysLog("database migration started")
		err = migrateDB()
		return err
	} else {
		common.FatalLog(err)
	}
	return err
}

// loadPersistedOptionalFeatureSettings reads only the current personal-edition
// feature options before schema creation. This lets a saved setting determine
// which optional tables are initialized on the next start without making the
// feature package depend on the model package.
func loadPersistedOptionalFeatureSettings() error {
	if DB == nil || !DB.Migrator().HasTable(&Option{}) {
		usage_mode.SetPersistedOptionalFeatures(nil)
		return nil
	}

	var options []Option
	if err := DB.Where("key IN ?", usage_mode.OptionalFeatureOptionKeys()).Find(&options).Error; err != nil {
		return fmt.Errorf("load optional feature settings: %w", err)
	}
	overrides := make(map[usage_mode.Feature]bool, len(options))
	for _, option := range options {
		if !usage_mode.IsOptionalFeatureOptionKey(option.Key) {
			continue
		}
		enabled, err := strconv.ParseBool(strings.TrimSpace(option.Value))
		if err != nil {
			return fmt.Errorf("invalid optional feature setting %s: %w", option.Key, err)
		}
		feature := usage_mode.Feature(strings.TrimPrefix(option.Key, "feature."))
		overrides[feature] = enabled
	}
	usage_mode.SetPersistedOptionalFeatures(overrides)
	return nil
}

func InitLogDB() (err error) {
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		common.SetLogDatabaseType(common.MainDatabaseType())
		initCol()
		return
	}
	db, dbType, err := chooseDB("LOG_SQL_DSN", true)
	if err == nil {
		common.SetLogDatabaseType(dbType)
		initCol()
		if common.DebugEnabled {
			db = db.Debug()
		}
		LOG_DB = db
		// If log DB is MySQL, also ensure Chinese-capable charset
		if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
			if err := checkMySQLChineseSupport(LOG_DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := LOG_DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		if !common.IsMasterNode {
			return nil
		}
		common.SysLog("database migration started")
		err = migrateLOGDB()
		return err
	} else {
		common.FatalLog(err)
	}
	return err
}

func migrateDB() error {
	// The core schema is deliberately small for a fresh personal installation.
	// Optional task/media/deployment tables are created only when their feature
	// is explicitly enabled before startup. We do not run legacy upgrade or
	// cleanup migrations here; an existing old New API database must be
	// re-created for this edition.
	if err := rejectLegacySchema(); err != nil {
		return err
	}
	coreModels := []interface{}{
		&Channel{},
		&Token{},
		&User{},
		&UserSession{},
		&Option{},
		&LoginEncryptionKey{},
		&Ability{},
		&Log{},
		&QuotaData{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&Setup{},
		&PerfMetric{},
		&BillingOperation{},
		&CasbinRule{},
		&AuthzRole{},
	}
	if err := DB.AutoMigrate(coreModels...); err != nil {
		return err
	}

	optionalModels := make([]interface{}, 0, 6)
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureSystemTasks) {
		optionalModels = append(optionalModels, &SystemTask{}, &SystemTaskLock{})
	}
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins) ||
		usage_mode.IsFeatureEnabled(usage_mode.FeatureMediaTasks) {
		optionalModels = append(optionalModels, &Task{})
	}
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins) {
		optionalModels = append(optionalModels, &TaskPlugin{})
	}
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureMediaTasks) {
		optionalModels = append(optionalModels, &Midjourney{})
	}
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureMultiNode) {
		optionalModels = append(optionalModels, &SystemInstance{})
	}
	if len(optionalModels) == 0 {
		return nil
	}
	err := DB.AutoMigrate(optionalModels...)
	if err != nil {
		return err
	}
	return nil
}

func rejectLegacySchema() error {
	if DB == nil {
		return fmt.Errorf("database is nil")
	}
	tables, err := DB.Migrator().GetTables()
	if err != nil {
		return fmt.Errorf("list database tables: %w", err)
	}
	persistentTableCount := 0
	for _, table := range tables {
		if table != "sqlite_sequence" {
			persistentTableCount++
		}
	}
	if persistentTableCount == 0 {
		return nil
	}
	if !DB.Migrator().HasTable(&Setup{}) ||
		!DB.Migrator().HasColumn(&Setup{}, "edition") ||
		!DB.Migrator().HasColumn(&Setup{}, "schema_version") {
		return fmt.Errorf("database is not a current ModelPort personal schema; use a new data directory")
	}
	for _, currentModel := range []interface{}{
		&Channel{}, &Token{}, &User{}, &UserSession{}, &Option{},
		&LoginEncryptionKey{}, &Ability{}, &Log{}, &QuotaData{},
		&Model{}, &Vendor{}, &PrefillGroup{}, &Setup{}, &PerfMetric{},
		&BillingOperation{}, &CasbinRule{}, &AuthzRole{},
	} {
		if !DB.Migrator().HasTable(currentModel) {
			return fmt.Errorf("database is missing a current core schema table; use a new data directory")
		}
	}
	var setup Setup
	err = DB.First(&setup).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var userCount int64
		if countErr := DB.Model(&User{}).Count(&userCount).Error; countErr != nil {
			return fmt.Errorf("validate uninitialized database: %w", countErr)
		}
		if userCount == 0 {
			return nil
		}
		return fmt.Errorf("database contains users but no current setup record; use a new data directory")
	}
	if err != nil {
		return fmt.Errorf("read setup record: %w", err)
	}
	if !setup.IsCurrentSchema() {
		return fmt.Errorf("database setup record does not identify the current ModelPort personal schema; use a new data directory")
	}
	return nil
}

func migrateLOGDB() error {
	if err := rejectLegacyLogSchema(); err != nil {
		return err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return migrateClickHouseLogDB()
	}
	return LOG_DB.AutoMigrate(&Log{})
}

func rejectLegacyLogSchema() error {
	if LOG_DB == nil {
		return fmt.Errorf("log database is nil")
	}
	if !LOG_DB.Migrator().HasTable(&Log{}) {
		return nil
	}
	for _, column := range []string{
		"id", "user_id", "created_at", "type", "content", "username",
		"token_name", "model_name", "quota", "prompt_tokens",
		"completion_tokens", "use_time", "is_stream", "channel_id",
		"token_id", "group", "ip", "request_id", "upstream_request_id",
		"billing_operation_key", "other",
	} {
		if !LOG_DB.Migrator().HasColumn(&Log{}, column) {
			return fmt.Errorf("log database is missing current logs.%s; use a new log database", column)
		}
	}
	if !common.UsingLogDatabase(common.DatabaseTypeClickHouse) &&
		!LOG_DB.Migrator().HasIndex(&Log{}, "idx_logs_billing_operation_key") {
		return fmt.Errorf("log database is missing the current billing operation index; use a new log database")
	}
	return nil
}

func migrateClickHouseLogDB() error {
	ttlDays := clickHouseLogTTLDays()
	if err := LOG_DB.Exec(clickHouseLogCreateTableSQL(ttlDays)).Error; err != nil {
		return err
	}
	return syncClickHouseLogTTL(ttlDays)
}

func clickHouseLogTTLDays() int {
	ttlDays := common.GetEnvOrDefault("LOG_SQL_CLICKHOUSE_TTL_DAYS", 0)
	if ttlDays < 0 {
		return 0
	}
	return ttlDays
}

func clickHouseLogTTLExpression(ttlDays int) string {
	if ttlDays <= 0 {
		return ""
	}
	return fmt.Sprintf("toDateTime(created_at) + INTERVAL %d DAY DELETE", ttlDays)
}

func clickHouseLogTTLClause(ttlDays int) string {
	expression := clickHouseLogTTLExpression(ttlDays)
	if expression == "" {
		return ""
	}
	return "\nTTL " + expression
}

func clickHouseLogCreateTableSQL(ttlDays int) string {
	return fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS logs (
	id Int64 DEFAULT 0,
	user_id Int32 DEFAULT 0,
	created_at Int64 DEFAULT 0,
	type Int32 DEFAULT 0,
	content String DEFAULT '',
	username String DEFAULT '',
	token_name String DEFAULT '',
	model_name String DEFAULT '',
	quota Int32 DEFAULT 0,
	prompt_tokens Int32 DEFAULT 0,
	completion_tokens Int32 DEFAULT 0,
	use_time Int32 DEFAULT 0,
	is_stream UInt8 DEFAULT 0,
	channel_id Int32 DEFAULT 0,
	token_id Int32 DEFAULT 0,
	`+"`group`"+` String DEFAULT '',
	ip String DEFAULT '',
	request_id String DEFAULT '',
	upstream_request_id String DEFAULT '',
	billing_operation_key Nullable(String) DEFAULT NULL,
	other String DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(toDateTime(created_at))
ORDER BY (created_at, request_id)%s`, clickHouseLogTTLClause(ttlDays))
}

func syncClickHouseLogTTL(ttlDays int) error {
	expression := clickHouseLogTTLExpression(ttlDays)
	if expression != "" {
		return LOG_DB.Exec("ALTER TABLE logs MODIFY TTL " + expression).Error
	}

	hasTTL, err := clickHouseLogTableHasTTL()
	if err != nil {
		return err
	}
	if !hasTTL {
		return nil
	}
	return LOG_DB.Exec("ALTER TABLE logs REMOVE TTL").Error
}

func clickHouseLogTableHasTTL() (bool, error) {
	var createTableSQL string
	if err := LOG_DB.Raw("SHOW CREATE TABLE logs").Scan(&createTableSQL).Error; err != nil {
		return false, err
	}
	return clickHouseCreateTableHasTTL(createTableSQL), nil
}

func clickHouseCreateTableHasTTL(createTableSQL string) bool {
	upperSQL := strings.ToUpper(createTableSQL)
	return strings.Contains(upperSQL, "\nTTL ") || strings.Contains(upperSQL, " TTL ")
}

func closeDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}

func CloseDB() error {
	if LOG_DB != DB {
		err := closeDB(LOG_DB)
		if err != nil {
			return err
		}
	}
	return closeDB(DB)
}

// checkMySQLChineseSupport ensures the MySQL connection and current schema
// default charset/collation can store Chinese characters. It allows common
// Chinese-capable charsets (utf8mb4, utf8, gbk, big5, gb18030) and panics otherwise.
func checkMySQLChineseSupport(db *gorm.DB) error {
	// 仅检测：当前库默认字符集/排序规则 + 各表的排序规则（隐含字符集）

	// Read current schema defaults
	var schemaCharset, schemaCollation string
	err := db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()").Row().Scan(&schemaCharset, &schemaCollation)
	if err != nil {
		return fmt.Errorf("读取当前库默认字符集/排序规则失败 / Failed to read schema default charset/collation: %v", err)
	}

	toLower := func(s string) string { return strings.ToLower(s) }
	// Allowed charsets that can store Chinese text
	allowedCharsets := map[string]string{
		"utf8mb4": "utf8mb4_",
		"utf8":    "utf8_",
		"gbk":     "gbk_",
		"big5":    "big5_",
		"gb18030": "gb18030_",
	}
	isChineseCapable := func(cs, cl string) bool {
		csLower := toLower(cs)
		clLower := toLower(cl)
		if prefix, ok := allowedCharsets[csLower]; ok {
			if clLower == "" {
				return true
			}
			return strings.HasPrefix(clLower, prefix)
		}
		// 如果仅提供了排序规则，尝试按排序规则前缀判断
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(clLower, prefix) {
				return true
			}
		}
		return false
	}

	// 1) 当前库默认值必须支持中文
	if !isChineseCapable(schemaCharset, schemaCollation) {
		return fmt.Errorf("当前库默认字符集/排序规则不支持中文：schema(%s/%s)。请将库设置为 utf8mb4/utf8/gbk/big5/gb18030 / Schema default charset/collation is not Chinese-capable: schema(%s/%s). Please set to utf8mb4/utf8/gbk/big5/gb18030",
			schemaCharset, schemaCollation, schemaCharset, schemaCollation)
	}

	// 2) 所有物理表的排序规则（隐含字符集）必须支持中文
	type tableInfo struct {
		Name      string
		Collation *string
	}
	var tables []tableInfo
	if err := db.Raw("SELECT TABLE_NAME, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("读取表排序规则失败 / Failed to read table collations: %v", err)
	}

	var badTables []string
	for _, t := range tables {
		// NULL 或空表示继承库默认设置，已在上面校验库默认，视为通过
		if t.Collation == nil || *t.Collation == "" {
			continue
		}
		cl := *t.Collation
		// 仅凭排序规则判断是否中文可用
		ok := false
		lower := strings.ToLower(cl)
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(lower, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			badTables = append(badTables, fmt.Sprintf("%s(%s)", t.Name, cl))
		}
	}

	if len(badTables) > 0 {
		// 限制输出数量以避免日志过长
		maxShow := 20
		shown := badTables
		if len(shown) > maxShow {
			shown = shown[:maxShow]
		}
		return fmt.Errorf(
			"存在不支持中文的表，请修复其排序规则/字符集。示例（最多展示 %d 项）：%v / Found tables not Chinese-capable. Please fix their collation/charset. Examples (showing up to %d): %v",
			maxShow, shown, maxShow, shown,
		)
	}
	return nil
}

var (
	lastPingTime time.Time
	pingMutex    sync.Mutex
)

func PingDB() error {
	pingMutex.Lock()
	defer pingMutex.Unlock()

	if time.Since(lastPingTime) < time.Second*10 {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Printf("Error getting sql.DB from GORM: %v", err)
		return err
	}

	err = sqlDB.Ping()
	if err != nil {
		log.Printf("Error pinging DB: %v", err)
		return err
	}

	lastPingTime = time.Now()
	common.SysLog("Database pinged successfully")
	return nil
}
