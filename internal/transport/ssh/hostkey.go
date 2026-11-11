package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/store"
	"github.com/wei-dong/tako/internal/transport"
)

// KnownHosts is Tako's own OpenSSH-format known_hosts file.
type KnownHosts struct {
	path string
	mu   sync.Mutex // serializes writes and reads of the file
}

// NewKnownHosts returns the store backed by path, which need not exist yet.
func NewKnownHosts(path string) *KnownHosts { return &KnownHosts{path: path} }

// Path returns the file's location.
func (k *KnownHosts) Path() string { return k.path }

// callback returns an ssh.HostKeyCallback for one target that applies policy,
// asking prompter about unknown keys and always refusing changed ones.
func (k *KnownHosts) callback(ctx context.Context, h model.Host, prompter transport.Prompter) gossh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		k.mu.Lock()
		verify, err := k.verifier()
		k.mu.Unlock()
		if err != nil {
			return err
		}
		err = verify(hostname, remote, key)
		if err == nil {
			return nil
		}
		keyErr, ok := errors.AsType[*knownhosts.KeyError](err)
		if !ok {
			return fmt.Errorf("checking host key of %s: %w", hostname, err)
		}
		fp := gossh.FingerprintSHA256(key)
		if len(keyErr.Want) > 0 {
			return &transport.HostKeyChangedError{
				Host: hostname,
				Old:  gossh.FingerprintSHA256(keyErr.Want[0].Key),
				New:  fp,
			}
		}
		if h.Policy() == model.HostKeyStrict {
			return &transport.HostKeyUnknownError{Host: hostname, Fingerprint: fp}
		}
		ok, perr := prompter.ConfirmHostKey(ctx, transport.HostKeyRequest{
			Host: hostname, Name: h.Name, KeyType: key.Type(), Fingerprint: fp,
		})
		if perr != nil {
			return fmt.Errorf("confirming host key: %w", perr)
		}
		if !ok {
			return fmt.Errorf("%w: %s (%s) was not trusted", transport.ErrHostKeyRejected, hostname, fp)
		}
		return k.Trust(hostname, remote, key)
	}
}

// verifier loads the file afresh so keys trusted a moment ago count. The
// caller holds k.mu.
func (k *KnownHosts) verifier() (gossh.HostKeyCallback, error) {
	if _, err := os.Stat(k.path); errors.Is(err, os.ErrNotExist) {
		return func(string, net.Addr, gossh.PublicKey) error {
			return &knownhosts.KeyError{}
		}, nil
	}
	cb, err := knownhosts.New(k.path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", k.path, err)
	}
	return cb, nil
}

// Trust records key as the trusted key of hostname, replacing any earlier
// entry for it. This is the only way a changed key becomes acceptable, and it
// is only called after an explicit user decision.
func (k *KnownHosts) Trust(hostname string, remote net.Addr, key gossh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.removeLocked(hostname); err != nil {
		return err
	}
	addrs := []string{hostname}
	if remote != nil {
		if ip := remote.String(); ip != hostname {
			addrs = append(addrs, ip)
		}
	}
	line := knownhosts.Line(addrs, key) + "\n"
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(k.path), err)
	}
	f, err := os.OpenFile(k.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", k.path, err)
	}
	_, werr := f.WriteString(line)
	if err := errors.Join(werr, f.Close()); err != nil {
		return fmt.Errorf("writing %s: %w", k.path, err)
	}
	return nil
}

// Forget removes every key of hostname.
func (k *KnownHosts) Forget(hostname string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.removeLocked(hostname)
}

// removeLocked rewrites the file without lines naming hostname.
func (k *KnownHosts) removeLocked(hostname string) error {
	data, err := os.ReadFile(k.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", k.path, err)
	}
	want := knownhosts.Normalize(hostname)
	var out []byte
	changed := false
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if lineNames(line, want) {
			changed = true
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	if !changed {
		return nil
	}
	if err := store.WriteFileAtomic(k.path, out); err != nil {
		return fmt.Errorf("rewriting %s: %w", k.path, err)
	}
	return nil
}

// lineNames reports whether a known_hosts line lists the normalized host.
func lineNames(line []byte, normalized string) bool {
	_, hosts, _, _, _, err := gossh.ParseKnownHosts(line)
	if err != nil {
		return false
	}
	return slices.Contains(hosts, normalized)
}

// algorithms returns the host key algorithms to offer for hostname, derived
// from the key types already trusted for it. It returns nil for an unknown
// host, which leaves the library defaults in place. Offering only the types we
// know makes the server present the key we can verify, as OpenSSH does.
func (k *KnownHosts) algorithms(hostname string) []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	data, err := os.ReadFile(k.path)
	if err != nil {
		return nil
	}
	want := knownhosts.Normalize(hostname)
	var algs []string
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		_, hosts, key, _, _, err := gossh.ParseKnownHosts(line)
		if err != nil || !slices.Contains(hosts, want) {
			continue
		}
		for _, a := range algorithmsOf(key.Type()) {
			if !slices.Contains(algs, a) {
				algs = append(algs, a)
			}
		}
	}
	return algs
}

// algorithmsOf maps a key type to the signature algorithms a server may use
// with it; RSA keys sign with SHA-2 variants.
func algorithmsOf(keyType string) []string {
	if keyType == gossh.KeyAlgoRSA {
		return []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA}
	}
	return []string{keyType}
}
