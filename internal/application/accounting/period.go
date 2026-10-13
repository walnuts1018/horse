package accounting

import "time"

var accountingLocation = time.FixedZone("JST", 9*60*60)

func PeriodFor(value time.Time) string {
	return value.In(accountingLocation).Format("2006-01")
}
