package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ConfigItem struct {
	Key   string `gorm:"primaryKey;column:key;type:text"`
	Value string `gorm:"column:value;type:text"`
}

func (ConfigItem) TableName() string {
	return "configs"
}

var (
	db    *gorm.DB
	SetDb = func(gdb *gorm.DB) {
		db = gdb
		migrateInPlace()
	}
)

func migrateInPlace() {
	db.AutoMigrate(&ConfigItem{})
}

func Get(key string, defaul ...any) (any, error) {
	var item ConfigItem
	err := db.First(&item, "key = ?", key).Error
	if err != nil {
		if len(defaul) > 0 {
			v := defaul[0]
			err = Set(key, v)
			return v, err
		}
		return nil, err
	}
	var val any
	if err := json.Unmarshal([]byte(item.Value), &val); err != nil {
		return nil, err
	}
	return val, nil
}

func GetAs[T any](key string, defaul ...any) (T, error) {
	var t T
	var item ConfigItem
	err := db.First(&item, "key = ?", key).Error
	if err != nil {
		if len(defaul) > 0 {
			if v, ok := defaul[0].(T); ok {
				err = Set(key, v)
				return v, err
			}
			val := reflect.ValueOf(&t).Elem()
			if err := convertAndSet(defaul[0], val); err != nil {
				return t, fmt.Errorf("default value type mismatch: expected %T, got %T", t, defaul[0])
			}
			err = Set(key, t)
			return t, err
		}
		return t, err
	}
	if err = json.Unmarshal([]byte(item.Value), &t); err != nil {
		var generic any
		if err := json.Unmarshal([]byte(item.Value), &generic); err != nil {
			return t, err
		}
		val := reflect.ValueOf(&t).Elem()
		if err := convertAndSet(generic, val); err != nil {
			return t, err
		}
	}
	return t, nil
}

func GetMany(keys map[string]any) (map[string]any, error) {
	var items []ConfigItem
	result := make(map[string]any)
	keyList := make([]string, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	if len(keyList) == 0 {
		return result, nil
	}
	if err := db.Where("key IN ?", keyList).Find(&items).Error; err != nil {
		return nil, err
	}
	foundKeys := make(map[string]bool)
	for _, item := range items {
		var parsed any
		if err := json.Unmarshal([]byte(item.Value), &parsed); err == nil {
			result[item.Key] = parsed
			foundKeys[item.Key] = true
		}
	}
	var toInsert []ConfigItem
	for k, def := range keys {
		if _, found := foundKeys[k]; !found && def != nil {
			result[k] = def
			jsonBytes, err := json.Marshal(def)
			if err != nil {
				slog.Warn("marshal default value failed", "key", k, "error", err)
				continue
			}
			toInsert = append(toInsert, ConfigItem{Key: k, Value: string(jsonBytes)})
		}
	}
	if len(toInsert) > 0 {
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value"}),
		}).Create(&toInsert)
	}
	return result, nil
}

func GetManyAs[T any]() (*T, error) {
	var t T
	val := reflect.ValueOf(&t).Elem()
	typ := val.Type()

	type fieldInfo struct {
		index      int
		key        string
		hasDefault bool
		defaultVal string
	}

	var fields []fieldInfo
	var keys []string

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		key := strings.Split(jsonTag, ",")[0]
		if key == "" || key == "-" {
			continue
		}
		defaultTag := field.Tag.Get("default")
		_, hasDefault := field.Tag.Lookup("default")
		fields = append(fields, fieldInfo{index: i, key: key, hasDefault: hasDefault, defaultVal: defaultTag})
		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return &t, nil
	}

	var items []ConfigItem
	if err := db.Where("key IN ?", keys).Find(&items).Error; err != nil {
		return nil, err
	}

	foundItems := make(map[string]string)
	for _, item := range items {
		foundItems[item.Key] = item.Value
	}

	var toInsert []ConfigItem
	for _, fi := range fields {
		fieldVal := val.Field(fi.index)
		if !fieldVal.CanSet() {
			continue
		}
		if dbValue, found := foundItems[fi.key]; found {
			if err := unmarshalToField(dbValue, fieldVal); err != nil {
				slog.Warn("unmarshal config failed", "key", fi.key, "error", err)
			}
		} else if fi.hasDefault {
			if err := parseDefaultToField(fi.defaultVal, fieldVal); err != nil {
				slog.Warn("parse default value failed", "key", fi.key, "error", err)
				continue
			}
			jsonBytes, err := json.Marshal(fieldVal.Interface())
			if err != nil {
				continue
			}
			toInsert = append(toInsert, ConfigItem{Key: fi.key, Value: string(jsonBytes)})
		}
	}

	if len(toInsert) > 0 {
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value"}),
		}).Create(&toInsert)
	}
	return &t, nil
}

func unmarshalToField(jsonStr string, fieldVal reflect.Value) error {
	target := reflect.New(fieldVal.Type()).Interface()
	if err := json.Unmarshal([]byte(jsonStr), target); err != nil {
		var generic any
		if err := json.Unmarshal([]byte(jsonStr), &generic); err != nil {
			return err
		}
		return convertAndSet(generic, fieldVal)
	}
	fieldVal.Set(reflect.ValueOf(target).Elem())
	return nil
}

func parseDefaultToField(defaultVal string, fieldVal reflect.Value) error {
	switch fieldVal.Kind() {
	case reflect.String:
		fieldVal.SetString(defaultVal)
	case reflect.Bool:
		fieldVal.SetBool(defaultVal == "true" || defaultVal == "1")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var v int64
		if defaultVal != "" {
			if _, err := fmt.Sscanf(defaultVal, "%d", &v); err != nil {
				var f float64
				if _, err2 := fmt.Sscanf(defaultVal, "%f", &f); err2 != nil {
					return err
				}
				v = int64(f)
			}
		}
		fieldVal.SetInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var v uint64
		if defaultVal != "" {
			if _, err := fmt.Sscanf(defaultVal, "%d", &v); err != nil {
				var f float64
				if _, err2 := fmt.Sscanf(defaultVal, "%f", &f); err2 != nil {
					return err
				}
				v = uint64(f)
			}
		}
		fieldVal.SetUint(v)
	case reflect.Float32, reflect.Float64:
		var v float64
		if defaultVal != "" {
			if _, err := fmt.Sscanf(defaultVal, "%f", &v); err != nil {
				return err
			}
		}
		fieldVal.SetFloat(v)
	default:
		if defaultVal == "" {
			return nil
		}
		target := reflect.New(fieldVal.Type()).Interface()
		if err := json.Unmarshal([]byte(defaultVal), target); err != nil {
			return err
		}
		fieldVal.Set(reflect.ValueOf(target).Elem())
	}
	return nil
}

func convertAndSet(val any, fieldVal reflect.Value) error {
	if val == nil {
		return nil
	}
	targetType := fieldVal.Type()
	v := reflect.ValueOf(val)
	if v.Type().AssignableTo(targetType) {
		fieldVal.Set(v)
		return nil
	}
	if v.Type().ConvertibleTo(targetType) {
		fieldVal.Set(v.Convert(targetType))
		return nil
	}
	if f, ok := val.(float64); ok {
		switch fieldVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fieldVal.SetInt(int64(f))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fieldVal.SetUint(uint64(f))
			return nil
		case reflect.Float32, reflect.Float64:
			fieldVal.SetFloat(f)
			return nil
		}
	}
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	target := reflect.New(targetType).Interface()
	if err := json.Unmarshal(b, target); err != nil {
		return err
	}
	fieldVal.Set(reflect.ValueOf(target).Elem())
	return nil
}

func GetAll() (map[string]any, error) {
	var items []ConfigItem
	result := make(map[string]any)
	if err := db.Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		var parsed any
		if err := json.Unmarshal([]byte(item.Value), &parsed); err == nil {
			result[item.Key] = parsed
		}
	}
	return result, nil
}

func Set(key string, value any) error {
	oldVal := map[string]any{}
	var oldItem ConfigItem
	if err := db.First(&oldItem, "key = ?", key).Error; err == nil {
		var parsed any
		if err := json.Unmarshal([]byte(oldItem.Value), &parsed); err == nil {
			oldVal[key] = parsed
		}
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}
	item := ConfigItem{Key: key, Value: string(bytes)}
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&item).Error
	if err != nil {
		return err
	}
	publishEvent(oldVal, map[string]any{key: value})
	return nil
}

func SetManyAs[T any](config T) error {
	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	typ := val.Type()
	var items []ConfigItem
	for i := 0; i < val.NumField(); i++ {
		fieldType := typ.Field(i)
		tag := fieldType.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		bytes, err := json.Marshal(val.Field(i).Interface())
		if err != nil {
			return fmt.Errorf("marshal field %s failed: %w", fieldType.Name, err)
		}
		items = append(items, ConfigItem{Key: tag, Value: string(bytes)})
	}
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	newVal := make(map[string]any, len(items))
	for _, it := range items {
		keys = append(keys, it.Key)
		var parsed any
		if err := json.Unmarshal([]byte(it.Value), &parsed); err == nil {
			newVal[it.Key] = parsed
		}
	}
	oldVal := map[string]any{}
	var oldItems []ConfigItem
	if err := db.Where("key IN ?", keys).Find(&oldItems).Error; err == nil {
		for _, oi := range oldItems {
			var parsed any
			if err := json.Unmarshal([]byte(oi.Value), &parsed); err == nil {
				oldVal[oi.Key] = parsed
			}
		}
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&items).Error
	if err != nil {
		return err
	}
	publishEvent(oldVal, newVal)
	return nil
}

func SetMany(cst map[string]any) error {
	var items []ConfigItem
	for k, v := range cst {
		bytes, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal key %s failed: %w", k, err)
		}
		items = append(items, ConfigItem{Key: k, Value: string(bytes)})
	}
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	newVal := make(map[string]any, len(items))
	for _, it := range items {
		keys = append(keys, it.Key)
		var parsed any
		if err := json.Unmarshal([]byte(it.Value), &parsed); err == nil {
			newVal[it.Key] = parsed
		}
	}
	oldVal := map[string]any{}
	var oldItems []ConfigItem
	if err := db.Where("key IN ?", keys).Find(&oldItems).Error; err == nil {
		for _, oi := range oldItems {
			var parsed any
			if err := json.Unmarshal([]byte(oi.Value), &parsed); err == nil {
				oldVal[oi.Key] = parsed
			}
		}
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&items).Error
	if err != nil {
		return err
	}
	publishEvent(oldVal, newVal)
	return nil
}

type ConfigEvent struct {
	Old map[string]any
	New map[string]any
}

func (e ConfigEvent) IsChanged(key string) bool {
	oldVal, oldOk := e.Old[key]
	newVal, newOk := e.New[key]
	if !oldOk && !newOk {
		return false
	}
	if oldOk != newOk {
		return true
	}
	return !reflect.DeepEqual(oldVal, newVal)
}

func IsChangedT[T any](e ConfigEvent, key string) (bool, T) {
	changed := e.IsChanged(key)
	var zero T
	val, ok := e.New[key]
	if !ok {
		val, ok = e.Old[key]
		if !ok {
			return changed, zero
		}
	}
	if val == nil {
		return changed, zero
	}
	if cast, ok := val.(T); ok {
		return changed, cast
	}
	targetType := reflect.TypeOf((*T)(nil)).Elem()
	v := reflect.ValueOf(val)
	if v.IsValid() {
		if v.Type().AssignableTo(targetType) {
			return changed, v.Interface().(T)
		}
		if v.Type().ConvertibleTo(targetType) {
			return changed, v.Convert(targetType).Interface().(T)
		}
	}
	if b, err := json.Marshal(val); err == nil {
		var out T
		if err := json.Unmarshal(b, &out); err == nil {
			return changed, out
		}
	}
	return changed, zero
}

type ConfigSubscriber func(event ConfigEvent)

var (
	subscribersMu sync.RWMutex
	subscribers   []ConfigSubscriber
)

func Subscribe(subscriber ConfigSubscriber) {
	subscribersMu.Lock()
	defer subscribersMu.Unlock()
	subscribers = append(subscribers, subscriber)
}

func publishEvent(oldVal, newVal map[string]any) {
	subscribersMu.RLock()
	defer subscribersMu.RUnlock()
	for _, sub := range subscribers {
		event := ConfigEvent{Old: oldVal, New: newVal}
		go sub(event)
	}
}
