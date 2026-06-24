package backends

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dslzl/gork/app/platform/config/tomlutil"
)

type TomlConfigBackend struct {
	path string
}

type TomlConfigVersion struct {
	Revision  int64  `json:"revision"`
	Source    string `json:"source"`
	UpdatedAt int64  `json:"updated_at"`
}

func NewTomlConfigBackend(path string) *TomlConfigBackend {
	return &TomlConfigBackend{path: path}
}

func (b *TomlConfigBackend) Load(context.Context) (map[string]any, error) {
	file, err := os.Open(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	defer file.Close()
	return tomlutil.Parse(file)
}

func (b *TomlConfigBackend) ApplyPatch(_ context.Context, patch map[string]any) error {
	return b.ApplyPatchWithSource(context.Background(), patch, "admin")
}

func (b *TomlConfigBackend) ApplyPatchWithSource(_ context.Context, patch map[string]any, source string) error {
	existing := map[string]any{}
	file, err := os.Open(b.path)
	if err == nil {
		existing, err = tomlutil.Parse(file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	merged := deepMergeTOML(existing, patch)
	return b.writeAtomic(merged, source)
}

func (b *TomlConfigBackend) writeAtomic(data map[string]any, source string) error {
	if source == "" {
		source = "admin"
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), "."+filepath.Base(b.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := fmt.Fprintf(tmp, "# Managed by gork. Admin writes normalize TOML and do not preserve comments.\n# last_update_source = %q\n# last_update_unix = %d\n\n", source, time.Now().Unix()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tomlutil.Write(tmp, data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, b.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return syncParentDir(filepath.Dir(b.path))
}

func (b *TomlConfigBackend) Clear(context.Context) error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(b.path, []byte{}, 0o644)
}

func (b *TomlConfigBackend) Version(context.Context) (any, error) {
	info, err := os.Stat(b.path)
	if err != nil {
		return float64(0), nil
	}
	return TomlConfigVersion{
		Revision:  info.ModTime().UnixNano(),
		Source:    "file",
		UpdatedAt: info.ModTime().Unix(),
	}, nil
}

func (b *TomlConfigBackend) Close(context.Context) error {
	return nil
}

func deepMergeTOML(base, override map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		baseNested, baseOK := result[key].(map[string]any)
		overrideNested, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			result[key] = deepMergeTOML(baseNested, overrideNested)
			continue
		}
		result[key] = value
	}
	return result
}

func syncParentDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer handle.Close()
	_ = handle.Sync()
	return nil
}
