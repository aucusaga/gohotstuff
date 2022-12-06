package libs

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"time"
)

func GenRandomID() uint64 {
	nano := time.Now().UnixNano()
	rand.Seed(nano)
	randNum1 := rand.Int63()
	randNum2 := rand.Int63()
	shift1 := rand.Intn(16) + 2
	shift2 := rand.Intn(8) + 1

	randId := ((randNum1 >> uint(shift1)) + (randNum2 >> uint(shift2)) + (nano >> 1)) &
		0x7FFFFFFFFFFFFFFF
	return uint64(randId)

}

func GetSum(b []byte) string {
	h := sha256.New()
	h.Write(b)
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}

func F(input []byte) string {
	return string(input)
}
