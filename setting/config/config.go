package config

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ConfigManager 统一管理所有配置
type ConfigManager struct {
	configs map[string]interface{}
	mutex   sync.RWMutex
}

var GlobalConfig = NewConfigManager()

func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		configs: make(map[string]interface{}),
	}
}

// Register 注册一个配置模块
func (cm *ConfigManager) Register(name string, config interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.configs[name] = config
}

// Get 获取指定配置模块
func (cm *ConfigManager) Get(name string) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.configs[name]
}

// Update applies a set of fields while holding the manager write lock. The
// detached parse in updateConfigFromMap guarantees callers observe either the
// old configuration or the complete new value.
func (cm *ConfigManager) Update(name string, configMap map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	config, ok := cm.configs[name]
	if !ok {
		return fmt.Errorf("config %q is not registered", name)
	}
	return updateConfigFromMap(config, configMap)
}

// Validate parses a configuration update against an isolated copy. It lets
// persistence callers reject malformed JSON before touching the database or
// the live configuration object.
func (cm *ConfigManager) Validate(name string, configMap map[string]string) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	config, ok := cm.configs[name]
	if !ok {
		return fmt.Errorf("config %q is not registered", name)
	}
	typeOfConfig := reflect.TypeOf(config)
	if typeOfConfig == nil || typeOfConfig.Kind() != reflect.Ptr || reflect.ValueOf(config).IsNil() {
		return fmt.Errorf("config must be a non-nil pointer")
	}
	copyConfig := reflect.New(typeOfConfig.Elem()).Interface()
	encoded, err := common.Marshal(config)
	if err != nil {
		return err
	}
	if err := common.Unmarshal(encoded, copyConfig); err != nil {
		return err
	}
	return updateConfigFromMap(copyConfig, configMap)
}

// LoadFromDB 从数据库加载配置
func (cm *ConfigManager) LoadFromDB(options map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	var firstErr error

	for name, config := range cm.configs {
		prefix := name + "."
		configMap := make(map[string]string)

		// 收集属于此配置的所有选项
		for key, value := range options {
			if strings.HasPrefix(key, prefix) {
				configKey := strings.TrimPrefix(key, prefix)
				configMap[configKey] = value
			}
		}

		// 如果找到配置项，则更新配置
		if len(configMap) > 0 {
			if err := updateConfigFromMap(config, configMap); err != nil {
				common.SysError("failed to update config " + name + ": " + err.Error())
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", name, err)
				}
			}
		}
	}

	return firstErr
}

// SaveToDB 将配置保存到数据库
func (cm *ConfigManager) SaveToDB(updateFunc func(key, value string) error) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for name, config := range cm.configs {
		configMap, err := configToMap(config)
		if err != nil {
			return err
		}

		for key, value := range configMap {
			dbKey := name + "." + key
			if err := updateFunc(dbKey, value); err != nil {
				return err
			}
		}
	}

	return nil
}

// 辅助函数：将配置对象转换为map
func configToMap(config interface{}) (map[string]string, error) {
	result := make(map[string]string)

	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := configFieldKey(fieldType)
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 处理不同类型的字段
		var strValue string
		switch field.Kind() {
		case reflect.String:
			strValue = field.String()
		case reflect.Bool:
			strValue = strconv.FormatBool(field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(field.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(field.Float(), 'f', -1, 64)
		case reflect.Ptr:
			// 处理指针类型：如果非 nil，序列化指向的值
			if !field.IsNil() {
				bytes, err := common.Marshal(field.Interface())
				if err != nil {
					return nil, err
				}
				strValue = string(bytes)
			} else {
				// nil 指针序列化为 "null"
				strValue = "null"
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			// 复杂类型使用JSON序列化
			bytes, err := common.Marshal(field.Interface())
			if err != nil {
				return nil, err
			}
			strValue = string(bytes)
		default:
			// 跳过不支持的类型
			continue
		}

		result[key] = strValue
	}

	return result, nil
}

// 辅助函数：从map更新配置对象
func updateConfigFromMap(config interface{}, configMap map[string]string) error {
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("config must be a non-nil pointer")
	}
	val = val.Elem()

	if val.Kind() != reflect.Struct {
		return fmt.Errorf("config must point to a struct")
	}

	// Parse into a detached value first. A malformed field must never leave a
	// partially applied configuration behind; publish the complete struct only
	// after every supplied field has been validated.
	updated := reflect.New(val.Type()).Elem()
	updated.Set(val)

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := updated.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := configFieldKey(fieldType)
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 检查map中是否有对应的值
		strValue, ok := configMap[key]
		if !ok {
			continue
		}

		// 根据字段类型设置值
		if !field.CanSet() {
			continue
		}

		if err := parseConfigField(field, strValue); err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}
	}

	// A few settings expose pointer-backed, concurrency-safe maps (for example
	// group ratios) and other packages retain those pointers. Preserve their
	// identity while applying the already-validated value, otherwise readers
	// would keep observing the old map after a successful option update.
	for i := 0; i < val.NumField(); i++ {
		fieldType := typ.Field(i)
		key := configFieldKey(fieldType)
		if key == "" || key == "-" {
			key = fieldType.Name
		}
		if _, ok := configMap[key]; !ok {
			continue
		}
		original := val.Field(i)
		parsed := updated.Field(i)
		if original.Kind() != reflect.Ptr || original.IsNil() || parsed.IsNil() {
			continue
		}
		encoded, err := common.Marshal(parsed.Interface())
		if err != nil {
			return fmt.Errorf("serialize validated value for %s: %w", key, err)
		}
		if err := common.Unmarshal(encoded, original.Interface()); err != nil {
			return fmt.Errorf("commit validated value for %s: %w", key, err)
		}
		parsed.Set(original)
	}

	val.Set(updated)
	return nil
}

func configFieldKey(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if index := strings.IndexByte(tag, ','); index >= 0 {
		tag = tag[:index]
	}
	return tag
}

func parseConfigField(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
		return nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(parsed)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, field.Type().Bits())
		if err != nil {
			// Existing option rows may contain integral float strings such as
			// "2.000000". Preserve that compatibility while rejecting fractions,
			// non-finite values, and values outside the field range.
			floatValue, floatErr := strconv.ParseFloat(value, 64)
			if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || math.Trunc(floatValue) != floatValue {
				return err
			}
			min, max := integerBounds(field.Type().Bits(), true)
			if floatValue < min || floatValue > max {
				return err
			}
			parsed = int64(floatValue)
		}
		field.SetInt(parsed)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, field.Type().Bits())
		if err != nil {
			floatValue, floatErr := strconv.ParseFloat(value, 64)
			if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || math.Trunc(floatValue) != floatValue {
				return err
			}
			_, max := integerBounds(field.Type().Bits(), false)
			if floatValue < 0 || floatValue > max {
				return err
			}
			parsed = uint64(floatValue)
		}
		field.SetUint(parsed)
		return nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, field.Type().Bits())
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			if err != nil {
				return err
			}
			return fmt.Errorf("non-finite number")
		}
		field.SetFloat(parsed)
		return nil
	case reflect.Ptr:
		if strings.TrimSpace(value) == "null" {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		parsed := reflect.New(field.Type().Elem())
		if err := common.Unmarshal([]byte(value), parsed.Interface()); err != nil {
			return err
		}
		field.Set(parsed)
		return nil
	case reflect.Map, reflect.Slice, reflect.Struct, reflect.Interface:
		parsed := reflect.New(field.Type())
		if err := common.Unmarshal([]byte(value), parsed.Interface()); err != nil {
			return err
		}
		field.Set(parsed.Elem())
		return nil
	default:
		return fmt.Errorf("unsupported field type %s", field.Type())
	}
}

func integerBounds(bits int, signed bool) (float64, float64) {
	if signed {
		if bits == 64 {
			return -float64(math.MaxInt64), float64(math.MaxInt64)
		}
		max := float64(uint64(1) << (bits - 1))
		return -max, max - 1
	}
	if bits == 64 {
		return 0, float64(math.MaxUint64)
	}
	return 0, float64(uint64(1)<<bits - 1)
}

// ConfigToMap 将配置对象转换为map（导出函数）
func ConfigToMap(config interface{}) (map[string]string, error) {
	return configToMap(config)
}

// UpdateConfigFromMap 从map更新配置对象（导出函数）
func UpdateConfigFromMap(config interface{}, configMap map[string]string) error {
	return updateConfigFromMap(config, configMap)
}

// ExportAllConfigs 导出所有已注册的配置为扁平结构
func (cm *ConfigManager) ExportAllConfigs() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	result := make(map[string]string)

	for name, cfg := range cm.configs {
		configMap, err := ConfigToMap(cfg)
		if err != nil {
			continue
		}

		// 使用 "模块名.配置项" 的格式添加到结果中
		for key, value := range configMap {
			result[name+"."+key] = value
		}
	}

	return result
}
