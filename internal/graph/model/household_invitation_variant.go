package model

type HouseholdInvitation struct {
	ID          string   `json:"id"`
	HouseholdID string   `json:"householdId"`
	ExpiresAt   DateTime `json:"expiresAt"`
	CreatedAt   DateTime `json:"createdAt"`
}

func (HouseholdInvitation) IsNode() {}

type HouseholdInvitationVariant interface {
	IsNode()
	IsHouseholdInvitation()
	GetID() string
	GetHouseholdID() string
	GetExpiresAt() DateTime
	GetCreatedAt() DateTime
}

func (invitation *HouseholdInvitation) GetID() string          { return invitation.ID }
func (invitation *HouseholdInvitation) GetHouseholdID() string { return invitation.HouseholdID }
func (invitation *HouseholdInvitation) GetExpiresAt() DateTime { return invitation.ExpiresAt }
func (invitation *HouseholdInvitation) GetCreatedAt() DateTime { return invitation.CreatedAt }

type PendingHouseholdInvitation struct {
	*HouseholdInvitation
}

func (PendingHouseholdInvitation) IsHouseholdInvitation() {}

type AcceptedHouseholdInvitation struct {
	*HouseholdInvitation
	AcceptedAt DateTime `json:"acceptedAt"`
}

func (AcceptedHouseholdInvitation) IsHouseholdInvitation() {}

type RevokedHouseholdInvitation struct {
	*HouseholdInvitation
	RevokedAt DateTime `json:"revokedAt"`
}

func (RevokedHouseholdInvitation) IsHouseholdInvitation() {}

type ExpiredHouseholdInvitation struct {
	*HouseholdInvitation
}

func (ExpiredHouseholdInvitation) IsHouseholdInvitation() {}
