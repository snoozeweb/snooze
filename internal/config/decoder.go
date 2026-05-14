package config

import (
	"reflect"
	"strconv"

	"github.com/go-viper/mapstructure/v2"

	"github.com/japannext/snooze/internal/config/schema"
)

// decoderConfig returns a mapstructure config preloaded with the hooks needed
// to honour the YAML and env-var conventions used by snooze (numeric durations
// in seconds, string-coerced fields, etc.).
func decoderConfig(out any) *mapstructure.DecoderConfig {
	return &mapstructure.DecoderConfig{
		Result:           out,
		TagName:          "koanf",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			numericDurationHook(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc(),
		),
	}
}

// numericDurationHook lets a bare float or int decode into a Duration field by
// re-presenting it as the equivalent seconds string ("172800s"). The legacy
// Python YAML files store every timedelta this way.
func numericDurationHook() mapstructure.DecodeHookFuncType {
	return func(from, to reflect.Type, data any) (any, error) {
		if to != reflect.TypeOf(schema.Duration(0)) {
			return data, nil
		}
		switch from.Kind() {
		case reflect.Float32, reflect.Float64:
			v := reflect.ValueOf(data).Float()
			return strconv.FormatFloat(v, 'f', -1, 64) + "s", nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v := reflect.ValueOf(data).Int()
			return strconv.FormatInt(v, 10) + "s", nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			v := reflect.ValueOf(data).Uint()
			return strconv.FormatUint(v, 10) + "s", nil
		}
		return data, nil
	}
}
