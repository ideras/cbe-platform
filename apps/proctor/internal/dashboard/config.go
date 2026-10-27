package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"exam-platform/shared/clientwire"
)

// Config is what the dashboard remembers between runs: the server address and the token file. The token is read from its
// file every time and never copied here.
type Config struct {
	// Server is the address of the server the dashboard last connected to; it connects there again at startup.
	Server string `json:"server_url,omitempty"`
	// TokenFile is the last token file the server accepted.
	TokenFile string `json:"token_file,omitempty"`
}

const maxConfigFile = 64 << 10

// DefaultConfigPath is the dashboard's configuration file in the user's configuration folder, for example
// ~/.config/cbe-portal-proctor/config.json.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cbe-portal-proctor", "config.json"), nil
}

// loadConfig reads the configuration file. A missing file is an empty configuration; an unreadable or invalid one is
// reported together with an empty configuration. Paths that are not absolute are dropped: they would depend on the folder
// the dashboard was started from. Keys it does not know are ignored, so a file written by an earlier version (which also
// kept the last roster file) still loads, and loses them the next time the settings are saved.
func loadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, errors.New("The dashboard settings file cannot be opened.")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigFile+1))
	if err != nil || len(data) > maxConfigFile {
		return Config{}, errors.New("The dashboard settings file cannot be read.")
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, errors.New("The dashboard settings file is not valid JSON and was ignored.")
	}
	if normalized, err := clientwire.ValidateServer(config.Server); err == nil {
		config.Server = normalized
	} else {
		config.Server = ""
	}
	if !filepath.IsAbs(config.TokenFile) {
		config.TokenFile = ""
	}
	return config, nil
}

// saveConfig replaces the configuration file atomically, creating its folder privately.
func saveConfig(path string, config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// remember records a change in the configuration file. A file that cannot be written never fails the action that
// caused it; the window is told instead. The caller holds s.mu.
func (s *Service) remember(update func(*Config)) {
	update(&s.config)
	if s.opts.ConfigFile == "" {
		return
	}
	if err := saveConfig(s.opts.ConfigFile, s.config); err != nil {
		s.configNote = "The dashboard settings could not be saved, so the server address and the token file will not be remembered."
		return
	}
	s.configNote = ""
}

// startFolder is the folder a file dialog opens in: the one holding path, when it still exists.
func startFolder(path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}
