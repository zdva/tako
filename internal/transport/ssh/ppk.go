package ssh

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // PPK version 2 defines SHA-1 key derivation and MAC; there is no choice
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	gossh "golang.org/x/crypto/ssh"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/transport"
)

// ErrBadPPK means a PuTTY key file is malformed or uses something unsupported.
var ErrBadPPK = errors.New("invalid PuTTY key file")

const ppkMaxSection = 1 << 20

// isPPK reports whether data looks like a PuTTY private key file.
func isPPK(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte("PuTTY-User-Key-File-"))
}

// parsePPK reads a PuTTY .ppk key (versions 2 and 3, RSA, Ed25519 and ECDSA),
// asking for the passphrase of an encrypted one.
func (a *authSet) parsePPK(idx int, auth model.Auth, data []byte) (gossh.Signer, error) {
	f, err := readPPK(data)
	if err != nil {
		return nil, fmt.Errorf("parsing key %s: %w", auth.KeyPath, err)
	}
	if !f.encrypted() {
		return f.signer(nil)
	}
	pass := a.storedSecret(auth)
	for try := range maxPassphraseTries {
		if pass == nil || try > 0 {
			var aerr error
			if pass, aerr = a.ask(idx, auth, "passphrase", auth.KeyPath); aerr != nil {
				return nil, aerr
			}
		}
		signer, err := f.signer(pass)
		if err == nil {
			clear(pass)
			return signer, nil
		}
		if !errors.Is(err, errPPKWrongPassphrase) {
			return nil, fmt.Errorf("parsing key %s: %w", auth.KeyPath, err)
		}
	}
	return nil, fmt.Errorf("%w: wrong passphrase for %s", transport.ErrAuthFailed, auth.KeyPath)
}

var errPPKWrongPassphrase = errors.New("wrong passphrase")

// ppkFile is a parsed .ppk before decryption.
type ppkFile struct {
	version             int
	keyType, encryption string
	comment             string
	pub, priv           []byte
	mac                 []byte
	kdf                 string
	argonMem, argonPass uint32
	argonPar            uint8
	salt                []byte
}

func (f *ppkFile) encrypted() bool { return f.encryption != "none" }

func readPPK(data []byte) (*ppkFile, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	f := &ppkFile{}
	i := 0
	next := func() (string, string, bool) {
		for i < len(lines) {
			line := lines[i]
			i++
			if strings.TrimSpace(line) == "" {
				continue
			}
			k, v, ok := strings.Cut(line, ": ")
			return k, strings.TrimSpace(v), ok
		}
		return "", "", false
	}
	readBlock := func(count string) ([]byte, error) {
		n, err := strconv.Atoi(count)
		if err != nil || n < 0 || n > ppkMaxSection/64 || i+n > len(lines) {
			return nil, fmt.Errorf("%w: bad line count %q", ErrBadPPK, count)
		}
		raw := strings.Join(lines[i:i+n], "")
		i += n
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadPPK, err)
		}
		return b, nil
	}
	for {
		k, v, ok := next()
		if !ok {
			break
		}
		var err error
		switch k {
		case "PuTTY-User-Key-File-2":
			f.version, f.keyType = 2, v
		case "PuTTY-User-Key-File-3":
			f.version, f.keyType = 3, v
		case "Encryption":
			f.encryption = v
		case "Comment":
			f.comment = v
		case "Public-Lines":
			f.pub, err = readBlock(v)
		case "Private-Lines":
			f.priv, err = readBlock(v)
		case "Private-MAC":
			f.mac, err = hex.DecodeString(v)
		case "Key-Derivation":
			f.kdf = v
		case "Argon2-Memory":
			f.argonMem, err = parseU32(v)
		case "Argon2-Passes":
			var n uint32
			n, err = parseU32(v)
			f.argonPass = n
		case "Argon2-Parallelism":
			var n uint32
			n, err = parseU32(v)
			f.argonPar = uint8(min(n, 255)) //nolint:gosec // clamped to the uint8 range on the line above
		case "Argon2-Salt":
			f.salt, err = hex.DecodeString(v)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrBadPPK, k, err)
		}
	}
	if f.version == 0 || f.keyType == "" || f.pub == nil || f.priv == nil || f.encryption == "" {
		return nil, fmt.Errorf("%w: missing fields", ErrBadPPK)
	}
	if f.encryption != "none" && f.encryption != "aes256-cbc" {
		return nil, fmt.Errorf("%w: unsupported encryption %q", ErrBadPPK, f.encryption)
	}
	return f, nil
}

func parseU32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing %q: %w", s, err)
	}
	return uint32(n), nil
}

// signer decrypts the private part (when encrypted), checks the MAC and builds
// the signer.
func (f *ppkFile) signer(pass []byte) (gossh.Signer, error) {
	priv := f.priv
	var macKey []byte
	if f.encrypted() {
		key, iv, mk, err := f.deriveKeys(pass)
		if err != nil {
			return nil, err
		}
		macKey = mk
		if len(priv)%aes.BlockSize != 0 {
			return nil, fmt.Errorf("%w: private section is not block aligned", ErrBadPPK)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadPPK, err)
		}
		plain := make([]byte, len(priv))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, priv)
		priv = plain
	} else if f.version == 2 {
		macKey = f.macKeyV2(nil)
	}
	if !f.macOK(macKey, priv) {
		if f.encrypted() {
			return nil, errPPKWrongPassphrase
		}
		return nil, fmt.Errorf("%w: MAC mismatch", ErrBadPPK)
	}
	key, err := buildPPKKey(f.keyType, f.pub, priv)
	if err != nil {
		return nil, err
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPPK, err)
	}
	return signer, nil
}

func (f *ppkFile) deriveKeys(pass []byte) (key, iv, macKey []byte, err error) {
	if f.version == 2 {
		h := func(n uint32) []byte {
			s := sha1.New() //nolint:gosec // dictated by the PPK v2 format
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], n)
			s.Write(b[:])
			s.Write(pass)
			return s.Sum(nil)
		}
		key = append(h(0), h(1)...)[:32]
		return key, make([]byte, aes.BlockSize), f.macKeyV2(pass), nil
	}
	var derive func(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte
	switch f.kdf {
	case "Argon2id":
		derive = argon2.IDKey
	case "Argon2i":
		derive = argon2.Key
	default:
		return nil, nil, nil, fmt.Errorf("%w: unsupported key derivation %q", ErrBadPPK, f.kdf)
	}
	if f.argonMem == 0 || f.argonPass == 0 || f.argonPar == 0 || f.argonMem > 1<<21 || f.argonPass > 1<<12 {
		return nil, nil, nil, fmt.Errorf("%w: unreasonable Argon2 parameters", ErrBadPPK)
	}
	out := derive(pass, f.salt, f.argonPass, f.argonMem, f.argonPar, 80)
	return out[:32], out[32:48], out[48:80], nil
}

func (f *ppkFile) macKeyV2(pass []byte) []byte {
	s := sha1.New() //nolint:gosec // dictated by the PPK v2 format
	s.Write([]byte("putty-private-key-file-mac-key"))
	s.Write(pass)
	return s.Sum(nil)
}

func (f *ppkFile) macOK(macKey, priv []byte) bool {
	var body []byte
	for _, part := range [][]byte{[]byte(f.keyType), []byte(f.encryption), []byte(f.comment), f.pub, priv} {
		body = binary.BigEndian.AppendUint32(body, uint32(len(part))) //nolint:gosec // sections are bounded by ppkMaxSection
		body = append(body, part...)
	}
	var sum []byte
	if f.version == 2 {
		m := hmac.New(sha1.New, macKey) //nolint:gosec // dictated by the PPK v2 format
		m.Write(body)
		sum = m.Sum(nil)
	} else {
		m := hmac.New(sha256.New, macKey)
		m.Write(body)
		sum = m.Sum(nil)
	}
	return subtle.ConstantTimeCompare(sum, f.mac) == 1
}

// ssh wire reader.
type wire struct{ b []byte }

func (w *wire) bytes() ([]byte, error) {
	if len(w.b) < 4 {
		return nil, fmt.Errorf("%w: truncated key data", ErrBadPPK)
	}
	n := binary.BigEndian.Uint32(w.b)
	if uint64(n) > uint64(len(w.b)-4) {
		return nil, fmt.Errorf("%w: truncated key data", ErrBadPPK)
	}
	out := w.b[4 : 4+n]
	w.b = w.b[4+n:]
	return out, nil
}

func (w *wire) mpint() (*big.Int, error) {
	b, err := w.bytes()
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b), nil
}

func buildPPKKey(keyType string, pub, priv []byte) (crypto.PrivateKey, error) {
	pr, pw := &wire{b: pub}, &wire{b: priv}
	if t, err := pr.bytes(); err != nil || string(t) != keyType {
		return nil, fmt.Errorf("%w: public blob does not match key type %s", ErrBadPPK, keyType)
	}
	switch {
	case keyType == "ssh-rsa":
		return buildRSA(pr, pw)
	case keyType == "ssh-ed25519":
		return buildEd25519(pr, pw)
	case strings.HasPrefix(keyType, "ecdsa-sha2-"):
		return buildECDSA(keyType, pr, pw)
	default:
		return nil, fmt.Errorf("%w: unsupported key type %s", ErrBadPPK, keyType)
	}
}

func buildRSA(pr, pw *wire) (crypto.PrivateKey, error) {
	e, err := pr.mpint()
	if err != nil {
		return nil, err
	}
	n, err := pr.mpint()
	if err != nil {
		return nil, err
	}
	d, err := pw.mpint()
	if err != nil {
		return nil, err
	}
	p, err := pw.mpint()
	if err != nil {
		return nil, err
	}
	q, err := pw.mpint()
	if err != nil {
		return nil, err
	}
	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: n, E: int(e.Int64())},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPPK, err)
	}
	key.Precompute()
	return key, nil
}

func buildEd25519(pr, pw *wire) (crypto.PrivateKey, error) {
	pubKey, err := pr.bytes()
	if err != nil || len(pubKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: bad Ed25519 public key", ErrBadPPK)
	}
	seed, err := pw.mpint()
	if err != nil {
		return nil, err
	}
	// PuTTY stores the 32-byte seed as an mpint, which drops leading zeros.
	raw := seed.FillBytes(make([]byte, ed25519.SeedSize))
	key := ed25519.NewKeyFromSeed(raw)
	if !bytes.Equal(key.Public().(ed25519.PublicKey), pubKey) { //nolint:forcetypeassert // ed25519.PrivateKey.Public always returns an ed25519.PublicKey
		return nil, fmt.Errorf("%w: Ed25519 private key does not match its public key", ErrBadPPK)
	}
	return key, nil
}

func buildECDSA(keyType string, pr, pw *wire) (crypto.PrivateKey, error) {
	curves := map[string]elliptic.Curve{
		"ecdsa-sha2-nistp256": elliptic.P256(),
		"ecdsa-sha2-nistp384": elliptic.P384(),
		"ecdsa-sha2-nistp521": elliptic.P521(),
	}
	curve, ok := curves[keyType]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported curve %s", ErrBadPPK, keyType)
	}
	if _, err := pr.bytes(); err != nil { // curve name
		return nil, err
	}
	point, err := pr.bytes()
	if err != nil {
		return nil, err
	}
	d, err := pw.mpint()
	if err != nil {
		return nil, err
	}
	x, y := elliptic.Unmarshal(curve, point) //nolint:staticcheck // no non-deprecated way to build an ecdsa.PrivateKey from x, y, d with big.Ints
	if x == nil {
		return nil, fmt.Errorf("%w: bad ECDSA public point", ErrBadPPK)
	}
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}, nil
}
