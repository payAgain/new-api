package model

import (
	"errors"

	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

// SaveWorkBuddyCredentials serializes rotation with channel edits and compares
// the old credential in Go, avoiding case-insensitive MySQL text comparisons.
func SaveWorkBuddyCredentials(db *gorm.DB, channelID int, oldKey, newKey string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ? AND type = ?", channelID, constant.ChannelTypeWorkBuddy).First(&channel).Error; err != nil {
			return errors.New("WorkBuddy channel no longer exists")
		}
		if channel.Key != oldKey {
			return errors.New("WorkBuddy account configuration changed; retry request")
		}
		if err := tx.Model(&Channel{}).Where("id = ?", channelID).Update("key", newKey).Error; err != nil {
			return errors.New("WorkBuddy refreshed credential could not be saved")
		}
		return nil
	})
}
