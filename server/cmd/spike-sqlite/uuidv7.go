package main

import (
	"encoding/hex"
	"math/rand"
	"sync"
	"time"
)

type uuidGen struct {
	mu  sync.Mutex
	rnd *rand.Rand
}

func newUUIDGen(seed int64) *uuidGen {
	return &uuidGen{rnd: rand.New(rand.NewSource(seed))}
}

func (g *uuidGen) nextAt(unixMilli int64) string {
	var b [16]byte
	b[0] = byte(unixMilli >> 40)
	b[1] = byte(unixMilli >> 32)
	b[2] = byte(unixMilli >> 24)
	b[3] = byte(unixMilli >> 16)
	b[4] = byte(unixMilli >> 8)
	b[5] = byte(unixMilli)

	g.mu.Lock()
	r1, r2, r3 := g.rnd.Uint64(), g.rnd.Uint64(), g.rnd.Uint64()
	g.mu.Unlock()

	putUint64(b[6:14], r1^r2)
	b[14] = byte(r3 >> 8)
	b[15] = byte(r3)

	b[6] = 0x70 | (b[6] & 0x0f)
	b[8] = 0x80 | (b[8] & 0x3f)

	return formatUUID(b)
}

func (g *uuidGen) next() string {
	return g.nextAt(time.Now().UnixMilli())
}

func putUint64(dst []byte, v uint64) {
	for i := 0; i < len(dst) && i < 8; i++ {
		dst[i] = byte(v >> (8 * (7 - i)))
	}
}

func formatUUID(b [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}
