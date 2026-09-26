package models

// Setting is a runtime-editable configuration value.
//
// Settings that hold credentials are stored encrypted (see internal/secrets),
// and the API never returns their plaintext — the admin UI shows a mask and
// can only overwrite, never read back. A value that can be read back from an
// admin screen is one screenshot away from being leaked.
type Setting struct {
	Base
	// Columns are setting_key / setting_group: KEY and GROUP are both reserved
	// words in MySQL (models/columns_test.go enforces this).
	Key   string `gorm:"column:setting_key;size:64;uniqueIndex;not null" json:"key"`
	Value string `gorm:"type:text" json:"-"`

	// Secret marks a value that is encrypted at rest and never serialised.
	Secret bool   `gorm:"default:false" json:"secret"`
	Group  string `gorm:"column:setting_group;size:32;index" json:"group"`
	Label  string `gorm:"size:128" json:"label"`

	UpdatedByID *uint `gorm:"index" json:"updatedById"`
}

// Setting keys. Anything listed in secretSettingKeys is encrypted at rest.
const (
	SettingTelegramBotToken   = "telegram.bot_token"
	SettingTelegramChannelID  = "telegram.channel_id"
	SettingTelegramAutoPublish = "telegram.auto_publish"
	SettingSiteName           = "site.name"
	SettingSiteNameKh         = "site.name_kh"
)

// SecretSettingKeys are stored encrypted and never returned in plaintext.
func SecretSettingKeys() map[string]bool {
	return map[string]bool{
		SettingTelegramBotToken: true,
	}
}
