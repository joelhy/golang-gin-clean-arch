package security

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"clean-arch-gin/user"
	"golang.org/x/crypto/argon2"
)

const (
	minimumPasswordMemory      = 19 * 1024
	maximumPasswordMemory      = 1024 * 1024
	maximumPasswordIterations  = 10
	maximumPasswordParallelism = 32
	minimumPasswordSaltLength  = 16
	maximumPasswordSaltLength  = 64
	minimumPasswordKeyLength   = 16
	maximumPasswordKeyLength   = 64

	// Older hashes may have used a shorter salt, but accepting fewer than eight
	// bytes provides too little uniqueness to be useful even during migration.
	minimumParsedSaltLength = 8
	maximumEncodedHashBytes = 512
)

var ErrInvalidPasswordHash = errors.New("invalid password hash")

type PasswordConfig struct {
	Memory      int64
	Iterations  int64
	Parallelism int64
	SaltLength  int64
	KeyLength   int64
}

type passwordParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

// PasswordManager hashes and verifies passwords using an immutable Argon2id policy.
type PasswordManager struct {
	parameters passwordParameters
	random     *lockedRandomReader
}

var _ user.Passwords = (*PasswordManager)(nil)

// NewPasswordManager takes exclusive ownership of reader so all salt reads use
// the manager's synchronization boundary.
func NewPasswordManager(cfg PasswordConfig, reader io.Reader) (*PasswordManager, error) {
	if err := validatePasswordConfig(cfg); err != nil {
		return nil, err
	}
	random, err := newLockedRandomReader(reader)
	if err != nil {
		return nil, err
	}

	// Configuration remains signed until all policy bounds pass. Converting earlier
	// would let negative or oversized decoded values wrap into an apparently valid
	// Argon2 allocation parameter.
	parameters := passwordParameters{
		memory:      uint32(cfg.Memory),
		iterations:  uint32(cfg.Iterations),
		parallelism: uint8(cfg.Parallelism),
		saltLength:  uint32(cfg.SaltLength),
		keyLength:   uint32(cfg.KeyLength),
	}
	return &PasswordManager{parameters: parameters, random: random}, nil
}

func (m *PasswordManager) Hash(password string) (string, error) {
	salt := make([]byte, m.parameters.saltLength)
	if err := m.random.readFull(salt, "generate password salt"); err != nil {
		return "", err
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		m.parameters.iterations,
		m.parameters.memory,
		m.parameters.parallelism,
		m.parameters.keyLength,
	)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		m.parameters.memory,
		m.parameters.iterations,
		m.parameters.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func (m *PasswordManager) Verify(encoded, password string) (bool, error) {
	parameters, salt, expected, err := parsePasswordHash(encoded, m.parameters)
	if err != nil {
		return false, ErrInvalidPasswordHash
	}

	actual := argon2.IDKey(
		[]byte(password),
		salt,
		parameters.iterations,
		parameters.memory,
		parameters.parallelism,
		parameters.keyLength,
	)
	// A constant-time comparison avoids making the matching hash prefix observable
	// through timing once the deliberately expensive Argon2 computation completes.
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func (m *PasswordManager) NeedsRehash(encoded string) bool {
	parameters, _, _, err := parsePasswordHash(encoded, m.parameters)
	if err != nil {
		return true
	}
	return parameters != m.parameters
}

func validatePasswordConfig(cfg PasswordConfig) error {
	switch {
	case cfg.Memory < minimumPasswordMemory || cfg.Memory > maximumPasswordMemory:
		return fmt.Errorf("password memory must be between %d and %d KiB", minimumPasswordMemory, maximumPasswordMemory)
	case cfg.Iterations < 1 || cfg.Iterations > maximumPasswordIterations:
		return fmt.Errorf("password iterations must be between 1 and %d", maximumPasswordIterations)
	case cfg.Parallelism < 1 || cfg.Parallelism > maximumPasswordParallelism:
		return fmt.Errorf("password parallelism must be between 1 and %d", maximumPasswordParallelism)
	case cfg.SaltLength < minimumPasswordSaltLength || cfg.SaltLength > maximumPasswordSaltLength:
		return fmt.Errorf("password salt length must be between %d and %d bytes", minimumPasswordSaltLength, maximumPasswordSaltLength)
	case cfg.KeyLength < minimumPasswordKeyLength || cfg.KeyLength > maximumPasswordKeyLength:
		return fmt.Errorf("password key length must be between %d and %d bytes", minimumPasswordKeyLength, maximumPasswordKeyLength)
	default:
		return nil
	}
}

func parsePasswordHash(encoded string, limits passwordParameters) (passwordParameters, []byte, []byte, error) {
	if len(encoded) == 0 || len(encoded) > maximumEncodedHashBytes {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" || fields[2] != "v=19" {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}

	parameters, err := parsePasswordParameters(fields[3])
	if err != nil {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	// The active policy is also the runtime work ceiling. This still permits
	// migration from weaker historical hashes while preventing a stored hash
	// from forcing more memory, CPU passes, or parallel lanes than configured.
	if parameters.memory > limits.memory || parameters.iterations > limits.iterations ||
		parameters.parallelism > limits.parallelism {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	// Segment lengths are checked before DecodeString so an attacker cannot make
	// base64 allocate according to an untrusted, arbitrarily large hash field.
	if !validBase64FieldLength(fields[4], minimumParsedSaltLength, maximumPasswordSaltLength) ||
		!validBase64FieldLength(fields[5], minimumPasswordKeyLength, maximumPasswordKeyLength) {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(fields[4])
	// DecodeString intentionally ignores CR/LF even in Strict mode. Re-encoding
	// must match byte-for-byte so non-canonical fields cannot reach Argon2.
	if err != nil || len(salt) < minimumParsedSaltLength || len(salt) > maximumPasswordSaltLength ||
		base64.RawStdEncoding.EncodeToString(salt) != fields[4] {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(fields[5])
	if err != nil || len(hash) < minimumPasswordKeyLength || len(hash) > maximumPasswordKeyLength ||
		base64.RawStdEncoding.EncodeToString(hash) != fields[5] {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	parameters.saltLength = uint32(len(salt))
	parameters.keyLength = uint32(len(hash))
	return parameters, salt, hash, nil
}

func parsePasswordParameters(encoded string) (passwordParameters, error) {
	fields := strings.Split(encoded, ",")
	if len(fields) != 3 {
		return passwordParameters{}, ErrInvalidPasswordHash
	}

	values := make(map[string]uint64, 3)
	for _, field := range fields {
		name, raw, found := strings.Cut(field, "=")
		if !found || (name != "m" && name != "t" && name != "p") {
			return passwordParameters{}, ErrInvalidPasswordHash
		}
		if _, duplicate := values[name]; duplicate {
			return passwordParameters{}, ErrInvalidPasswordHash
		}
		value, err := parseStrictUint(raw)
		if err != nil {
			return passwordParameters{}, ErrInvalidPasswordHash
		}
		values[name] = value
	}

	memory, memoryOK := values["m"]
	iterations, iterationsOK := values["t"]
	parallelism, parallelismOK := values["p"]
	if !memoryOK || !iterationsOK || !parallelismOK ||
		memory > maximumPasswordMemory ||
		iterations < 1 || iterations > maximumPasswordIterations ||
		parallelism < 1 || parallelism > maximumPasswordParallelism ||
		memory < 8*parallelism {
		return passwordParameters{}, ErrInvalidPasswordHash
	}

	return passwordParameters{
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
	}, nil
}

func parseStrictUint(raw string) (uint64, error) {
	if raw == "" {
		return 0, ErrInvalidPasswordHash
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, ErrInvalidPasswordHash
		}
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, ErrInvalidPasswordHash
	}
	return value, nil
}

func validBase64FieldLength(field string, minimum, maximum int) bool {
	return len(field) >= base64.RawStdEncoding.EncodedLen(minimum) &&
		len(field) <= base64.RawStdEncoding.EncodedLen(maximum)
}
