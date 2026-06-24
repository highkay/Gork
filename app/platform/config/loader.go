package config

import (
	"os"
	"strings"

	"github.com/dslzl/gork/app/platform/config/tomlutil"
)

type LoadConfigOptions struct {
	UserPath  string
	EnvPrefix string
	Env       map[string]string
}

func FlattenConfig(mapping map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range mapping {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			for nestedKey, nestedValue := range FlattenConfig(nested, full) {
				out[nestedKey] = nestedValue
			}
			continue
		}
		out[full] = value
	}
	return out
}

func DeepMergeConfig(base, override map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		baseNested, baseOK := result[key].(map[string]any)
		overrideNested, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			result[key] = DeepMergeConfig(baseNested, overrideNested)
			continue
		}
		result[key] = value
	}
	return result
}

func LoadTOML(path string) (map[string]any, error) {
	if path == "" {
		return map[string]any{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	defer file.Close()
	return tomlutil.Parse(file)
}

func LoadConfig(defaultsPath string, options LoadConfigOptions) (map[string]any, error) {
	data, err := LoadTOML(defaultsPath)
	if err != nil {
		return nil, err
	}
	if options.UserPath != "" {
		if user, err := LoadTOML(options.UserPath); err != nil {
			return nil, err
		} else if len(user) > 0 {
			data = DeepMergeConfig(data, user)
		}
	}
	prefix := options.EnvPrefix
	if prefix == "" {
		prefix = "GROK_"
	}
	return ApplyEnvConfig(data, prefix, options.Env), nil
}

func GetNested(data map[string]any, dottedKey string, defaultValue any) any {
	var node any = data
	for _, key := range strings.Split(dottedKey, ".") {
		mapping, ok := node.(map[string]any)
		if !ok {
			return defaultValue
		}
		value, ok := mapping[key]
		if !ok || value == nil {
			return defaultValue
		}
		node = value
	}
	return node
}

func SetNested(data map[string]any, dottedKey string, value any) {
	parts := strings.Split(dottedKey, ".")
	current := data
	for _, key := range parts[:len(parts)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func loadConfigEnv(env map[string]string) map[string]string {
	if env != nil {
		return env
	}
	out := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
