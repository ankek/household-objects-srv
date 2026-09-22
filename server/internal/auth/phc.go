package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strconv"
	"strings"
)

var ErrMalformedHash = errors.New("auth: stored password hash is not a readable argon2id hash")

var b64 = base64.RawStdEncoding

var decodeLimits = struct {
	minMemoryKiB, maxMemoryKiB     uint32
	minTime, maxTime               uint32
	minParallelism, maxParallelism uint8
	minSaltLen, maxSaltLen         int
	minKeyLen, maxKeyLen           int
}{
	minMemoryKiB: 8, maxMemoryKiB: 64 * 1024,
	minTime: 1, maxTime: 64,
	minParallelism: 1, maxParallelism: 8,
	minSaltLen: 8, maxSaltLen: 64,
	minKeyLen: 16, maxKeyLen: 64,
}

type decodedHash struct {
	params Params
	salt   []byte
	key    []byte
}

func (p Params) encode(salt, key []byte) string {
	var b strings.Builder
	b.WriteString("$argon2id$v=")
	b.WriteString(strconv.Itoa(argon2.Version))
	b.WriteString("$m=")
	b.WriteString(strconv.FormatUint(uint64(p.MemoryKiB), 10))
	b.WriteString(",t=")
	b.WriteString(strconv.FormatUint(uint64(p.Time), 10))
	b.WriteString(",p=")
	b.WriteString(strconv.FormatUint(uint64(p.Parallelism), 10))
	b.WriteByte('$')
	b.WriteString(b64.EncodeToString(salt))
	b.WriteByte('$')
	b.WriteString(b64.EncodeToString(key))
	return b.String()
}

func decode(encoded string) (decodedHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return decodedHash{}, fmt.Errorf("%w: expected 5 '$'-separated fields, got %d", ErrMalformedHash, len(parts)-1)
	}

	if parts[1] != "argon2id" {
		return decodedHash{}, fmt.Errorf("%w: algorithm is not argon2id", ErrMalformedHash)
	}

	version, err := strconv.ParseUint(strings.TrimPrefix(parts[2], "v="), 10, 32)
	if !strings.HasPrefix(parts[2], "v=") || err != nil {
		return decodedHash{}, fmt.Errorf("%w: unreadable version field", ErrMalformedHash)
	}
	if version != uint64(argon2.Version) {
		return decodedHash{}, fmt.Errorf("%w: argon2 version %d is not %d", ErrMalformedHash, version, argon2.Version)
	}

	var memory, time uint64
	var parallelism uint64
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 || !strings.HasPrefix(fields[0], "m=") || !strings.HasPrefix(fields[1], "t=") || !strings.HasPrefix(fields[2], "p=") {
		return decodedHash{}, fmt.Errorf("%w: parameter field is not m=,t=,p=", ErrMalformedHash)
	}
	if memory, err = strconv.ParseUint(strings.TrimPrefix(fields[0], "m="), 10, 32); err != nil {
		return decodedHash{}, fmt.Errorf("%w: unreadable m", ErrMalformedHash)
	}
	if time, err = strconv.ParseUint(strings.TrimPrefix(fields[1], "t="), 10, 32); err != nil {
		return decodedHash{}, fmt.Errorf("%w: unreadable t", ErrMalformedHash)
	}
	if parallelism, err = strconv.ParseUint(strings.TrimPrefix(fields[2], "p="), 10, 8); err != nil {
		return decodedHash{}, fmt.Errorf("%w: unreadable p", ErrMalformedHash)
	}

	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return decodedHash{}, fmt.Errorf("%w: salt is not unpadded base64", ErrMalformedHash)
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return decodedHash{}, fmt.Errorf("%w: digest is not unpadded base64", ErrMalformedHash)
	}

	d := decodedHash{
		params: Params{
			MemoryKiB:   uint32(memory),
			Time:        uint32(time),
			Parallelism: uint8(parallelism),
			SaltLength:  len(salt),
			KeyLength:   len(key),
		},
		salt: salt,
		key:  key,
	}
	if err := d.params.withinDecodeLimits(); err != nil {
		return decodedHash{}, err
	}
	return d, nil
}

func (p Params) withinDecodeLimits() error {
	if err := p.checkLimits(); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformedHash, err)
	}
	return nil
}

func (p Params) checkLimits() error {
	l := decodeLimits
	switch {
	case p.MemoryKiB < l.minMemoryKiB || p.MemoryKiB > l.maxMemoryKiB:
		return fmt.Errorf("m=%d outside %d..%d KiB", p.MemoryKiB, l.minMemoryKiB, l.maxMemoryKiB)
	case p.Time < l.minTime || p.Time > l.maxTime:
		return fmt.Errorf("t=%d outside %d..%d", p.Time, l.minTime, l.maxTime)
	case p.Parallelism < l.minParallelism || p.Parallelism > l.maxParallelism:
		return fmt.Errorf("p=%d outside %d..%d", p.Parallelism, l.minParallelism, l.maxParallelism)
	case p.SaltLength < l.minSaltLen || p.SaltLength > l.maxSaltLen:
		return fmt.Errorf("salt is %d bytes, outside %d..%d", p.SaltLength, l.minSaltLen, l.maxSaltLen)
	case p.KeyLength < l.minKeyLen || p.KeyLength > l.maxKeyLen:
		return fmt.Errorf("digest is %d bytes, outside %d..%d", p.KeyLength, l.minKeyLen, l.maxKeyLen)
	case uint64(p.MemoryKiB) < 8*uint64(p.Parallelism):
		return fmt.Errorf("m=%d is below the 8*p=%d argon2 requires", p.MemoryKiB, 8*uint32(p.Parallelism))
	}
	return nil
}
