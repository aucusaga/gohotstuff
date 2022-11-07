package libs

import (
	"github.com/sony/sonyflake"
)

func GenRandomID() (uint64, error) {
	flake := sonyflake.NewSonyflake(sonyflake.Settings{})
	id, err := flake.NextID()
	if err != nil {
		return 0, err
	}
	return id, nil
}
