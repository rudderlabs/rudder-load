package producer

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func getRequiredStringSetting(m map[string]string, setting string) (string, error) {
	v, ok := m[setting]
	if v == "" || !ok {
		return "", fmt.Errorf("missing required setting %q", setting)
	}
	return v, nil
}

func getOptionalStringSetting(m map[string]string, setting, defaultValue string) (string, error) {
	v, ok := m[setting]
	if v == "" || !ok {
		return defaultValue, nil
	}
	return v, nil
}

func getOptionalIntSetting(m map[string]string, setting string, defaultValue int64) (int64, error) {
	v, ok := m[setting]
	if v == "" || !ok {
		return defaultValue, nil
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid int for setting %q: %v", setting, err)
	}
	return i, nil
}

func getOptionalBoolSetting(m map[string]string, setting string, defaultValue bool) (bool, error) {
	v, ok := m[setting]
	if v == "" || !ok {
		return defaultValue, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid bool for setting %q: %v", setting, err)
	}
	return b, nil
}

func getOptionalDurationSetting(m map[string]string, setting string, defaultValue time.Duration) (time.Duration, error) {
	v, ok := m[setting]
	if v == "" || !ok {
		return defaultValue, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration for setting %q: %v", setting, err)
	}
	return d, nil
}

func readConfiguration(prefix string, environ []string) (map[string]string, error) {
	m := make(map[string]string)
	for _, v := range environ {
		if strings.Index(v, prefix) != 0 {
			continue
		}
		kv := strings.Split(v, "=")
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid pulsar config %q", v)
		}
		m[strings.ToLower(kv[0][len(prefix):])] = kv[1]
	}
	return m, nil
}
