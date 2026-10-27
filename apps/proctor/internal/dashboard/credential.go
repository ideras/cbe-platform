package dashboard

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// chosenCredential is a token read from a file the proctor picked; it lives only in Go.
type chosenCredential struct {
	name, path, token string
	// confirm means the path is remembered once the server has accepted the token; a file given at launch or remembered
	// earlier needs no new record.
	confirm bool
}

// loadRemembered reads the configuration file, picks up the server and the token file that worked last time, and decides
// whether to connect at startup. A server or token given at launch wins over the remembered ones. Anything unusable is
// reported in the notice and otherwise ignored.
func (s *Service) loadRemembered() {
	s.tokenPath = s.opts.TokenFile
	remembered := false
	if s.opts.ConfigFile != "" {
		config, err := loadConfig(s.opts.ConfigFile)
		if err != nil {
			s.configNote = err.Error()
		}
		s.config = config
		if s.tokenPath == "" {
			s.tokenPath = config.TokenFile
		}
		if !s.opts.ServerSet && config.Server != "" {
			s.view.Server, remembered = config.Server, true
		}
		if s.opts.Token == "" && config.TokenFile != "" {
			if token, err := ReadCredential(config.TokenFile); err != nil {
				s.tokenNote = "The token file used last time (" + credentialLabel(config.TokenFile) + ") cannot be used any more. Choose it again."
			} else {
				s.pendingCredential = &chosenCredential{name: credentialLabel(config.TokenFile), path: config.TokenFile, token: token}
			}
		}
	}
	s.autoConnect = (s.opts.ServerSet || remembered) && (s.opts.Token != "" || s.pendingCredential != nil)
}

// CredentialChoice reports the token file the proctor picked. Cancelled means they closed the dialog. File is the folder and
// file name (several folders can hold a file called admin-token); the token itself is never returned.
type CredentialChoice struct {
	Cancelled bool   `json:"cancelled"`
	File      string `json:"file"`
}

func credentialLabel(path string) string {
	return filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
}

// ChooseCredential lets the proctor pick the file that holds the server's admin token (the one the server reads with
// -admin-token-file). Go reads and keeps the token and the next Connect uses it, so it is never typed, pasted or shown in the
// window. The file must pass the same private-file checks as the launch option.
func (s *Service) ChooseCredential() (CredentialChoice, error) {
	s.mu.Lock()
	dialog := FileDialog{Title: "Choose the proctor token file", Directory: startFolder(s.tokenPath)}
	s.mu.Unlock()
	path, err := s.pick(dialog, false)
	if err != nil {
		return CredentialChoice{}, err
	}
	if path == "" {
		return CredentialChoice{Cancelled: true}, nil
	}
	choice := CredentialChoice{File: credentialLabel(path)}
	s.mu.Lock()
	s.tokenPath = path // the next dialog opens here even if this file turns out to be unusable
	s.mu.Unlock()
	token, err := ReadCredential(path)
	if err != nil {
		return choice, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return choice, errors.New("Dashboard is closing.")
	}
	s.pendingCredential = &chosenCredential{name: choice.File, path: path, token: token, confirm: true}
	s.tokenNote = ""
	return choice, nil
}

func ReadCredential(path string) (string, error) {
	failure := errors.New("Proctor credential must be a private regular file with 32–4096 non-whitespace characters.")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", failure
	}
	file, err := os.Open(path)
	if err != nil {
		return "", failure
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4098))
	if err != nil || len(data) > 4097 {
		return "", failure
	}
	token := strings.TrimSpace(string(data))
	if !validToken(token) {
		return "", failure
	}
	return token, nil
}
