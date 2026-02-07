package storage

import "github.com/google/uuid"

type FeeTier struct {
	ID          uuid.UUID
	Name        string
	MakerFeeBps int
	TakerFeeBps int
	MinVolume   string
}
