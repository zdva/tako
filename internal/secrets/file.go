package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zdva/tako/internal/store"
)

const (
	fileVersion    = 1
	keyLen         = 32
	maxSecretBytes = 4096
	maxRefBytes    = 256
	hkdfInfo       = "tako-secrets-v1"
	keyFileName    = "secrets.key"

	bindingMachine = "machine"
	bindingFile    = "file"
)

// File is Tako's encrypted secret store. The on-disk file is an envelope of
// AES-256-GCM ciphertext; plaintext values are never written. A File is safe
// for concurrent use.
type File struct {
	path    string
	keyPath string
	machine func() ([]byte, error)

	mu          sync.Mutex
	warnFileKey sync.Once
	binding     string
	salt        []byte
	material    []byte
	key         []byte
	items       map[string][]byte
	closed      bool
}

// Open opens the encrypted store at path. A missing file is an empty store;
// the file is created on the first [File.Set].
//
// When the file cannot be decrypted, Open renames it to
// "<path>.unreadable-<time>" and returns an empty store together with an
// error wrapping [ErrSealed]. The caller can warn and keep going: the next
// Set starts a new file, and the old bytes stay in the renamed one.
//
// When the file is bound to this machine but the machine id cannot be read,
// Open returns [ErrBindingUnavailable] and leaves the file where it is. A
// file written by a newer Tako returns [ErrTooNew], also untouched.
func Open(path string) (*File, error) {
	return open(path, platformBinding)
}

func open(path string, machine func() ([]byte, error)) (*File, error) {
	f := newFile(path, machine)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the secret store: %w", err)
	}
	if err := f.unlock(data); err != nil {
		// Only a file whose contents cannot be decrypted is moved aside.
		// A missing machine id, a newer version, or a key file that cannot
		// be read (permissions) leaves the ciphertext where it is.
		if !errors.Is(err, ErrSealed) {
			return nil, err
		}
		aside := path + ".unreadable-" + time.Now().Format("20060102-150405")
		if rerr := os.Rename(path, aside); rerr != nil {
			return nil, fmt.Errorf("setting aside an unreadable secret store: %w", errors.Join(err, rerr))
		}
		return newFile(path, machine), fmt.Errorf("%w: moved to %s: %w", ErrSealed, filepath.Base(aside), err)
	}
	return f, nil
}

func newFile(path string, machine func() ([]byte, error)) *File {
	return &File{
		path:    path,
		keyPath: filepath.Join(filepath.Dir(path), keyFileName),
		machine: machine,
		items:   map[string][]byte{},
	}
}

// Get implements [Store]. The returned slice is a copy.
func (f *File) Get(ctx context.Context, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reading secret: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.disabled(); err != nil {
		return nil, err
	}
	v, ok := f.items[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return slices.Clone(v), nil
}

// Set implements [Store]. It writes the whole file before returning.
func (f *File) Set(ctx context.Context, ref string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}
	if err := validateRef(ref); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}
	if len(value) > maxSecretBytes {
		return fmt.Errorf("storing secret: %w", ErrTooLarge)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.disabled(); err != nil {
		return err
	}
	if err := f.ensureKey(); err != nil {
		return err
	}
	prev := cloneItems(f.items)
	cloned := slices.Clone(value)
	f.items[ref] = cloned
	if err := f.flush(); err != nil {
		clear(cloned)
		f.items = prev
		return err
	}
	return nil
}

// Delete implements [Store]. Deleting a missing secret is not an error.
func (f *File) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.disabled(); err != nil {
		return err
	}
	v, ok := f.items[ref]
	if !ok {
		return nil
	}
	prev := cloneItems(f.items)
	clear(v)
	delete(f.items, ref)
	if f.key == nil {
		return nil
	}
	if err := f.flush(); err != nil {
		f.items = prev
		return err
	}
	return nil
}

// Close drops the key and the in-memory copies. The file stays. Later calls fail.
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	clear(f.key)
	clear(f.material)
	clear(f.salt)
	f.key = nil
	f.material = nil
	f.salt = nil
	for ref, v := range f.items {
		clear(v)
		delete(f.items, ref)
	}
	return nil
}

func (f *File) disabled() error {
	if f.closed {
		return errClosed
	}
	return nil
}

func (f *File) ensureKey() error {
	if f.key != nil {
		return nil
	}
	material, binding, err := f.newMaterial()
	if err != nil {
		return err
	}
	salt := make([]byte, keyLen)
	if _, err := rand.Read(salt); err != nil {
		clear(material)
		return fmt.Errorf("creating the secret store: %w", err)
	}
	key, err := deriveKey(material, salt)
	if err != nil {
		clear(material)
		clear(salt)
		return err
	}
	f.material = material
	f.binding = binding
	f.salt = salt
	f.key = key
	return nil
}

func (f *File) newMaterial() ([]byte, string, error) {
	b, err := f.machineID()
	if err == nil {
		return b, bindingMachine, nil
	}
	f.warnFileKey.Do(func() {
		slog.Warn("machine id unavailable; the secret store key is a local file", "err", err)
	})
	key, kerr := loadOrCreateKeyFile(f.keyPath)
	if kerr != nil {
		return nil, "", kerr
	}
	return key, bindingFile, nil
}

func (f *File) machineID() ([]byte, error) {
	if f.machine == nil {
		return nil, errors.New("no machine id source")
	}
	b, err := f.machine()
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("empty machine id")
	}
	return slices.Clone(b), nil
}

func (f *File) unlock(data []byte) error {
	env, err := parseEnvelope(data)
	if err != nil {
		return err
	}
	material, err := f.materialFor(env.Binding)
	if err != nil {
		return err
	}
	key, err := deriveKey(material, env.salt)
	if err != nil {
		clear(material)
		return err
	}
	plain, err := unseal(key, env.nonce, env.data, aadFor(env.Binding))
	if err != nil {
		clear(material)
		clear(key)
		return err
	}
	defer clear(plain)
	items, err := decodePayload(plain)
	if err != nil {
		clear(material)
		clear(key)
		return err
	}
	f.binding = env.Binding
	f.salt = env.salt
	f.material = material
	f.key = key
	f.items = items
	return nil
}

func (f *File) materialFor(binding string) ([]byte, error) {
	switch binding {
	case bindingMachine:
		b, err := f.machineID()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBindingUnavailable, err)
		}
		return b, nil
	case bindingFile:
		b, err := readKeyFile(f.keyPath)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: missing key file", ErrSealed)
		}
		if err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, fmt.Errorf("%w: unknown binding", ErrSealed)
	}
}

func (f *File) flush() error {
	plain, err := encodePayload(f.items)
	if err != nil {
		return err
	}
	defer clear(plain)
	nonce, ciphertext, err := seal(f.key, plain, aadFor(f.binding))
	if err != nil {
		return err
	}
	env := envelope{
		Version: fileVersion,
		Binding: f.binding,
		Salt:    base64.StdEncoding.EncodeToString(f.salt),
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(ciphertext),
	}
	data, err := json.Marshal(env, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encoding the secret store: %w", err)
	}
	if err := store.WriteFileAtomic(f.path, append(data, '\n')); err != nil {
		return fmt.Errorf("writing the secret store: %w", err)
	}
	return nil
}

type envelope struct {
	Version int    `json:"version"`
	Binding string `json:"binding"`
	Salt    string `json:"salt"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

type parsedEnvelope struct {
	Binding     string
	salt, nonce []byte
	data        []byte
}

func parseEnvelope(raw []byte) (parsedEnvelope, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return parsedEnvelope{}, fmt.Errorf("%w: %w", ErrSealed, err)
	}
	if env.Version > fileVersion {
		return parsedEnvelope{}, fmt.Errorf("%w: file is version %d, this Tako reads %d",
			ErrTooNew, env.Version, fileVersion)
	}
	if env.Version != fileVersion {
		return parsedEnvelope{}, fmt.Errorf("%w: version %d", ErrSealed, env.Version)
	}
	if env.Binding != bindingMachine && env.Binding != bindingFile {
		return parsedEnvelope{}, fmt.Errorf("%w: unknown binding", ErrSealed)
	}
	salt, err := decodeField(env.Salt)
	if err != nil {
		return parsedEnvelope{}, err
	}
	nonce, err := decodeField(env.Nonce)
	if err != nil {
		return parsedEnvelope{}, err
	}
	data, err := decodeField(env.Data)
	if err != nil {
		return parsedEnvelope{}, err
	}
	if len(salt) < 16 {
		return parsedEnvelope{}, fmt.Errorf("%w: salt is too short", ErrSealed)
	}
	return parsedEnvelope{Binding: env.Binding, salt: salt, nonce: nonce, data: data}, nil
}

func decodeField(s string) ([]byte, error) {
	v, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSealed, err)
	}
	return v, nil
}

type secretPayload struct {
	Items map[string]string `json:"items"`
}

func encodePayload(items map[string][]byte) ([]byte, error) {
	payload := secretPayload{Items: make(map[string]string, len(items))}
	for ref, v := range items {
		payload.Items[ref] = base64.StdEncoding.EncodeToString(v)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding the secret store: %w", err)
	}
	return data, nil
}

func decodePayload(plain []byte) (map[string][]byte, error) {
	var payload secretPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSealed, err)
	}
	out := make(map[string][]byte, len(payload.Items))
	for ref, b64 := range payload.Items {
		v, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSealed, err)
		}
		out[ref] = v
	}
	return out, nil
}

func deriveKey(material, salt []byte) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, material, salt, hkdfInfo, keyLen)
	if err != nil {
		return nil, fmt.Errorf("deriving the secret store key: %w", err)
	}
	return key, nil
}

func aadFor(binding string) []byte {
	return []byte(hkdfInfo + ":" + binding)
}

func seal(key, plain, aad []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypting the secret store: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypting the secret store: %w", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("encrypting the secret store: %w", err)
	}
	return nonce, gcm.Seal(nil, nonce, plain, aad), nil
}

func unseal(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSealed, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSealed, err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrSealed
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSealed, err)
	}
	return plain, nil
}

func readKeyFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("reading the secret store key: %w", err)
	}
	if len(b) != keyLen {
		clear(b)
		return nil, fmt.Errorf("%w: key file has the wrong length", ErrSealed)
	}
	_ = os.Chmod(path, 0o600)
	return b, nil
}

func loadOrCreateKeyFile(path string) ([]byte, error) {
	b, err := readKeyFile(path)
	if err == nil {
		return b, nil
	}
	// A missing or unusable key is replaced. This runs only while creating a
	// store, never while opening one that already has ciphertext.
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, ErrSealed) {
		return nil, err
	}
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("creating the secret store key: %w", err)
	}
	if err := store.WriteFileAtomic(path, key); err != nil {
		clear(key)
		return nil, fmt.Errorf("creating the secret store key: %w", err)
	}
	return key, nil
}

func validateRef(ref string) error {
	switch {
	case ref == "":
		return errors.New("empty secret reference")
	case len(ref) > maxRefBytes:
		return errors.New("secret reference is too long")
	case strings.ContainsRune(ref, 0):
		return errors.New("secret reference contains NUL")
	default:
		return nil
	}
}

func cloneItems(items map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(items))
	for ref, v := range items {
		out[ref] = slices.Clone(v)
	}
	return out
}
