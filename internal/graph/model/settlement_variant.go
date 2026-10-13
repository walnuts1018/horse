package model

type Settlement struct {
	ID          string            `json:"id"`
	HouseholdID string            `json:"householdId"`
	Period      YearMonth         `json:"period"`
	Revision    int               `json:"revision"`
	Lines       []*SettlementLine `json:"lines"`
	CreatedAt   *DateTime         `json:"createdAt,omitempty"`
}

func (Settlement) IsNode() {}

type SettlementVariant interface {
	IsNode()
	IsSettlement()
	GetID() string
	GetHouseholdID() string
	GetPeriod() YearMonth
	GetRevision() int
	GetLines() []*SettlementLine
}

func (settlement *Settlement) GetID() string               { return settlement.ID }
func (settlement *Settlement) GetHouseholdID() string      { return settlement.HouseholdID }
func (settlement *Settlement) GetPeriod() YearMonth        { return settlement.Period }
func (settlement *Settlement) GetRevision() int            { return settlement.Revision }
func (settlement *Settlement) GetLines() []*SettlementLine { return settlement.Lines }

type OpenSettlement struct {
	*Settlement
}

func (OpenSettlement) IsSettlement() {}

type ClosedSettlement struct {
	*Settlement
}

func (ClosedSettlement) IsSettlement() {}
