package ssh

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/transport"
)

// maxPassphraseTries is how often a wrong key passphrase is asked again.
const maxPassphraseTries = 3

// authSet builds the ssh authentication methods for one host and owns the
// resources they open (the agent connection), which close with the transport.
type authSet struct {
	ctx      context.Context //nolint:containedctx // the dial's context governs prompts raised from library callbacks
	host     model.Host
	secrets  secrets.Store
	prompter transport.Prompter
	remember func(h model.Host, idx int, ref string)
	agentSrc func() (net.Conn, error)

	mu      sync.Mutex
	closers []io.Closer
}

func (a *authSet) addCloser(c io.Closer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closers = append(a.closers, c)
}

// Close releases what the methods opened. It implements io.Closer so the
// transport can own it.
func (a *authSet) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.closers {
		_ = c.Close() //nolint:errcheck // releasing an agent socket; nothing to do on failure
	}
	a.closers = nil
	return nil
}

// methods returns the host's authentication methods in order.
func (a *authSet) methods() []gossh.AuthMethod {
	var out []gossh.AuthMethod
	for i, auth := range a.host.Auth {
		switch auth.Kind {
		case model.AuthPassword:
			out = append(out, a.password(i, auth))
		case model.AuthKey:
			out = append(out, gossh.PublicKeysCallback(a.keySigners(i, auth)))
		case model.AuthAgent:
			out = append(out, gossh.PublicKeysCallback(a.agentSigners))
		case model.AuthKeyboardInteractive:
			out = append(out, gossh.KeyboardInteractive(a.challenge(i, auth)))
		}
	}
	return out
}

// storedSecret returns the secret saved for an auth entry, or nil.
func (a *authSet) storedSecret(auth model.Auth) []byte {
	if auth.SecretRef == "" || a.secrets == nil {
		return nil
	}
	v, err := a.secrets.Get(a.ctx, auth.SecretRef)
	if err != nil {
		return nil
	}
	return v
}

// ask prompts for a secret and stores it when the user asks to remember it.
func (a *authSet) ask(idx int, auth model.Auth, kind, detail string) ([]byte, error) {
	reply, err := a.prompter.Secret(a.ctx, transport.SecretRequest{Host: a.host, Kind: kind, Detail: detail})
	if err != nil {
		return nil, fmt.Errorf("asking for the %s of %s: %w", kind, a.host.Name, err)
	}
	if reply.Remember && a.secrets != nil {
		ref := auth.SecretRef
		if ref == "" {
			ref = model.SecretRef(a.host.ID, kindOf(auth)+suffix(idx))
		}
		// The user's choice to remember is best effort: a keychain that is
		// unavailable must not fail the connection. The secrets.Layered store
		// keeps the value for the session in that case.
		if err := a.secrets.Set(a.ctx, ref, reply.Value); err == nil || errors.Is(err, secrets.ErrNotPersisted) {
			if a.remember != nil {
				a.remember(a.host, idx, ref)
			}
		}
	}
	return reply.Value, nil
}

func kindOf(auth model.Auth) string {
	if auth.Kind == model.AuthKey {
		return "key-passphrase"
	}
	return string(auth.Kind)
}

func suffix(idx int) string {
	if idx == 0 {
		return ""
	}
	return fmt.Sprintf("-%d", idx)
}

func (a *authSet) password(idx int, auth model.Auth) gossh.AuthMethod {
	attempt := 0
	cb := func() (string, error) {
		attempt++
		if attempt == 1 {
			if v := a.storedSecret(auth); v != nil {
				return string(v), nil
			}
		}
		v, err := a.ask(idx, auth, "password", "")
		if err != nil {
			return "", err
		}
		return string(v), nil
	}
	return gossh.RetryableAuthMethod(gossh.PasswordCallback(cb), 3)
}

func (a *authSet) challenge(idx int, auth model.Auth) gossh.KeyboardInteractiveChallenge {
	first := true
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		if len(questions) == 0 {
			return nil, nil
		}
		if first && len(questions) == 1 && !echos[0] {
			first = false
			if v := a.storedSecret(auth); v != nil {
				return []string{string(v)}, nil
			}
		}
		first = false
		answers, err := a.prompter.Challenge(a.ctx, transport.ChallengeRequest{
			Host: a.host, Name: name, Instruction: instruction, Questions: questions, Echo: echos,
		})
		if err != nil {
			return nil, fmt.Errorf("answering the challenge of %s: %w", a.host.Name, err)
		}
		_ = idx
		return answers, nil
	}
}

func (a *authSet) agentSigners() ([]gossh.Signer, error) {
	open := a.agentSrc
	if open == nil {
		open = defaultAgentConn
	}
	conn, err := open()
	if err != nil {
		return nil, fmt.Errorf("connecting to ssh-agent: %w", err)
	}
	a.addCloser(conn)
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return nil, fmt.Errorf("listing ssh-agent keys: %w", err)
	}
	return signers, nil
}

// defaultAgentConn connects to the agent named by SSH_AUTH_SOCK.
func defaultAgentConn() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", sock, err)
	}
	return conn, nil
}

func (a *authSet) keySigners(idx int, auth model.Auth) func() ([]gossh.Signer, error) {
	return func() ([]gossh.Signer, error) {
		path := expandHome(auth.KeyPath)
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading key %s: %w", auth.KeyPath, err)
		}
		signer, err := a.parseKey(idx, auth, pem)
		if err != nil {
			return nil, err
		}
		return []gossh.Signer{signer}, nil
	}
}

// parseKey parses a private key, asking for its passphrase when needed.
func (a *authSet) parseKey(idx int, auth model.Auth, pem []byte) (gossh.Signer, error) {
	if isPPK(pem) {
		return a.parsePPK(idx, auth, pem)
	}
	signer, err := gossh.ParsePrivateKey(pem)
	if err == nil {
		return signer, nil
	}
	if _, ok := errors.AsType[*gossh.PassphraseMissingError](err); !ok {
		return nil, fmt.Errorf("parsing key %s: %w", auth.KeyPath, err)
	}
	pass := a.storedSecret(auth)
	for try := range maxPassphraseTries {
		if pass == nil || try > 0 {
			var aerr error
			if pass, aerr = a.ask(idx, auth, "passphrase", auth.KeyPath); aerr != nil {
				return nil, aerr
			}
		}
		signer, err = gossh.ParsePrivateKeyWithPassphrase(pem, pass)
		if err == nil {
			clear(pass)
			return signer, nil
		}
		if !errors.Is(err, x509.IncorrectPasswordError) {
			return nil, fmt.Errorf("parsing key %s: %w", auth.KeyPath, err)
		}
	}
	return nil, fmt.Errorf("%w: wrong passphrase for %s", transport.ErrAuthFailed, auth.KeyPath)
}

// expandHome expands a leading "~/" to the user's home directory.
func expandHome(p string) string {
	rest, ok := strings.CutPrefix(p, "~/")
	if !ok {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, rest)
}
