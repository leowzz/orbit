package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("read crypto random bytes: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func NewEpoch() string {
	return newID()
}
