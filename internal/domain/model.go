package domain

import (
	"context"
)

const MaxSafeInteger int64 = 9007199254740991

type AppError struct {
	Message string
	Status  int
}

func (e *AppError) Error() string               { return e.Message }
func NewError(status int, message string) error { return &AppError{Message: message, Status: status} }

type Config struct {
	Game      string `json:"game"`
	Front     int    `json:"front"`
	Back      int    `json:"back"`
	Count     int    `json:"count"`
	Multiple  int64  `json:"multiple"`
	Periods   int    `json:"periods"`
	Append    bool   `json:"append"`
	SameFront int    `json:"sameFront"`
	SameBack  int    `json:"sameBack"`
}
type Ticket struct {
	Front []int `json:"front"`
	Back  []int `json:"back"`
}
type Payload struct {
	Config          Config   `json:"config"`
	Tickets         []Ticket `json:"tickets"`
	Issues          []string `json:"issues"`
	MatchMode       string   `json:"matchMode"`
	PlacedAt        string   `json:"placedAt"`
	Note            string   `json:"note"`
	TotalAmount     float64  `json:"totalAmount"`
	AmountPerPeriod float64  `json:"amountPerPeriod"`
}
type Bet struct {
	Payload
	ID             int64    `json:"id"`
	CreatedAt      string   `json:"createdAt"`
	Status         string   `json:"status"`
	Results        []Result `json:"results"`
	CheckedPeriods int      `json:"checkedPeriods"`
	KnownWinAmount float64  `json:"knownWinAmount"`
	WinAmount      *float64 `json:"winAmount"`
}
type Draw struct {
	Game         string           `json:"game"`
	Issue        string           `json:"issue"`
	Date         string           `json:"date"`
	SalesCloseAt string           `json:"salesCloseAt,omitempty"`
	Front        []int            `json:"front"`
	Back         []int            `json:"back"`
	Prizes       map[int]*float64 `json:"prizes"`
	AppendPrizes map[int]*float64 `json:"appendPrizes"`
}
type Winner struct {
	Level            int      `json:"level"`
	Count            int64    `json:"count"`
	UnitAmount       *float64 `json:"unitAmount"`
	AppendUnitAmount *float64 `json:"appendUnitAmount"`
	Amount           *float64 `json:"amount"`
}
type TicketResult struct {
	FrontHits   int      `json:"frontHits"`
	BackHits    int      `json:"backHits"`
	Winners     []Winner `json:"winners"`
	Complete    bool     `json:"complete"`
	KnownAmount float64  `json:"knownAmount"`
	Amount      *float64 `json:"amount"`
}
type Result struct {
	Issue       string         `json:"issue"`
	Draw        Draw           `json:"draw"`
	Tickets     []TicketResult `json:"tickets"`
	Complete    bool           `json:"complete"`
	Won         bool           `json:"won"`
	KnownAmount float64        `json:"knownAmount"`
	Amount      *float64       `json:"amount"`
	CheckedAt   string         `json:"checkedAt,omitempty"`
}

type ListOptions struct {
	Page, PageSize int
	Game, Status   string
}
type BetPage struct {
	Items    []Bet `json:"items"`
	Total    int   `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}
type PendingTask struct {
	ID                         int64
	Game, Issue, PlacedAt, Key string
}
type Evaluation struct {
	ID     int64
	Result Result
}

type SyncState struct {
	Game, WindowDate, HistoryFrom, HistoryThrough string
	Complete                                      bool
}

type Provider interface {
	GetDraw(context.Context, string, string) (*Draw, error)
	GetHistory(context.Context, string, string, string) ([]Draw, error)
}
type OfficialSnapshot struct {
	Draws                   []Draw
	Published               []Draw
	HasUnready              bool
	Latest                  *Draw
	LatestIssue, LatestDate string
	LatestReady             bool
}

// SnapshotProvider adds official publication state without changing the
// legacy provider contract used by local consumers and existing integrations.
type SnapshotProvider interface {
	GetSnapshot(context.Context, string, string, string) (OfficialSnapshot, error)
}

func OfficialAmountsComplete(draw Draw) bool {
	levels := 6
	switch draw.Game {
	case "ssq":
	case "dlt":
		levels = 7
		if draw.Issue < "26014" {
			levels = 9
		}
	default:
		return false
	}
	complete := func(amount *float64) bool { cents, err := Cents(amount); return err == nil && cents != nil }
	for level := 1; level <= levels; level++ {
		if !complete(draw.Prizes[level]) {
			return false
		}
	}
	return draw.Game != "dlt" || complete(draw.AppendPrizes[1]) && complete(draw.AppendPrizes[2])
}
